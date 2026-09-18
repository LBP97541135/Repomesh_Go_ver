// Package manager implements the G1/G2 consumption half of the Manager
// pipeline: resolving an issue's pinned configuration revision into the exact
// model call inputs (provider base URL, model id, credentials reference) at
// request time. It never falls back to the project's current configuration;
// a missing or revoked material keeps the consumption blocked with reasons.
package manager

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"repomesh.local/repomesh/internal/projects"
)

// ModelCallConfig is the resolved per-issue model call input. SecretMaterial
// carries the secret version identity only; the plaintext is opened through
// the secrets store at send time and never persisted here.
type ModelCallConfig struct {
	IssueID          string
	ProjectID        string
	ConfigurationRev string
	ModelID          string
	ProviderID       string
	ProviderRevision string
	BaseURL          string
	APIFormat        string
	SecretVersionID  string
	ExecutionRecipe  *projects.ExecutionRecipe
	ResolvedAt       time.Time
}

// Blocker records why consumption is blocked; reasons are durable and shown
// to authorized readers, never silently substituted with a newer version.
type Blocker struct {
	Code   string
	Reason string
}

// ConsumptionResult distinguishes a usable configuration from a blocked one.
type ConsumptionResult struct {
	Config   *ModelCallConfig
	Blockers []Blocker
}

// Service resolves per-issue configuration consumption.
type Service struct {
	pool     *pgxpool.Pool
	projects *projects.Service
}

func New(pool *pgxpool.Pool, projectService *projects.Service) *Service {
	return &Service{pool: pool, projects: projectService}
}

// ResolveForIssue reads the issue's initial_configuration_revision and resolves
// the immutable closure (design §8 of issue-configuration-binding): issue ->
// ProjectConfigRevision -> model binding -> secret version. Any missing or
// restricted material blocks with explicit reasons; there is no fallback.
func (s *Service) ResolveForIssue(ctx context.Context, issueID, projectID string) (ConsumptionResult, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return ConsumptionResult{}, unavailable()
	}
	defer tx.Rollback(ctx)
	var configurationRevision string
	err = tx.QueryRow(ctx, `SELECT initial_configuration_revision FROM repomesh_issues.issues
		WHERE project_id=$1 AND id=$2 AND removed_at IS NULL`, projectID, issueID).Scan(&configurationRevision)
	if errors.Is(err, pgx.ErrNoRows) {
		return ConsumptionResult{}, failure(404, "RESOURCE_NOT_FOUND")
	}
	if err != nil {
		return ConsumptionResult{}, unavailable()
	}
	config, blockers := s.resolveClosure(ctx, tx, projectID, configurationRevision, issueID)
	if err := tx.Commit(ctx); err != nil {
		return ConsumptionResult{}, unavailable()
	}
	if len(blockers) > 0 {
		return ConsumptionResult{Blockers: blockers}, nil
	}
	return ConsumptionResult{Config: config}, nil
}

// resolveClosure walks the immutable closure for one pinned revision. Every
// step either resolves from immutable history or produces a durable blocker;
// nothing here reads the project's *current* configuration.
func (s *Service) resolveClosure(ctx context.Context, tx pgx.Tx, projectID, configurationRevision, issueID string) (*ModelCallConfig, []Blocker) {
	blockers := []Blocker{}
	config := ModelCallConfig{IssueID: issueID, ProjectID: projectID, ConfigurationRev: configurationRevision, ResolvedAt: time.Now().UTC()}
	var modelProfileID, modelProfileVersion, executionProfileID, executionProfileVersion *string
	err := tx.QueryRow(ctx, `SELECT model_profile_id, model_profile_version, execution_profile_id, execution_profile_version
		FROM repomesh_projects.configuration_revisions WHERE project_id=$1 AND revision=$2`,
		projectID, configurationRevision).Scan(&modelProfileID, &modelProfileVersion, &executionProfileID, &executionProfileVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, append(blockers, Blocker{Code: "CONFIGURATION_REVISION_MISSING", Reason: "pinned configuration revision no longer exists"})
	}
	if err != nil {
		return nil, append(blockers, Blocker{Code: "RESULT_UNCONFIRMED", Reason: "configuration lookup failed"})
	}
	if modelProfileID == nil || modelProfileVersion == nil {
		blockers = append(blockers, Blocker{Code: "MODEL_CONFIG_MISSING", Reason: "pinned configuration carries no model binding"})
		return nil, blockers
	}
	// Model binding snapshot: profile version -> provider revision -> base url + secret.
	var providerID, providerRevision, modelRowID, secretVersion string
	err = tx.QueryRow(ctx, `SELECT pl.provider_id, pl.provider_revision, pl.row_id, pl.secret_version_id
		FROM repomesh_models.profile_links pl
		WHERE pl.profile_id=$1 AND pl.profile_version=$2`,
		*modelProfileID, *modelProfileVersion).Scan(&providerID, &providerRevision, &modelRowID, &secretVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, append(blockers, Blocker{Code: "MODEL_CONFIG_MISSING", Reason: "pinned model profile version unavailable"})
	}
	if err != nil {
		return nil, append(blockers, Blocker{Code: "RESULT_UNCONFIRMED", Reason: "model binding lookup failed"})
	}
	config.ProviderID, config.ProviderRevision, config.SecretVersionID = providerID, providerRevision, secretVersion
	// The outbound model id is the immutable snapshot's model_id, not the row key.
	var baseURL, apiFormat, outboundModelID string
	err = tx.QueryRow(ctx, `SELECT pr.base_url, pr.api_format, ms.model_id FROM repomesh_models.provider_revisions pr
		JOIN repomesh_models.model_snapshots ms ON ms.provider_id=pr.provider_id AND ms.provider_revision=pr.revision
		JOIN repomesh_models.model_rows mr ON mr.provider_id=ms.provider_id AND mr.id=ms.row_id
		WHERE pr.provider_id=$1 AND pr.revision=$2 AND ms.row_id=$3`, providerID, providerRevision, modelRowID).Scan(&baseURL, &apiFormat, &outboundModelID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, append(blockers, Blocker{Code: "PROVIDER_REVISION_MISSING", Reason: "pinned provider revision unavailable"})
	}
	if err != nil {
		return nil, append(blockers, Blocker{Code: "RESULT_UNCONFIRMED", Reason: "provider lookup failed"})
	}
	config.BaseURL, config.APIFormat, config.ModelID = baseURL, apiFormat, outboundModelID
	if executionProfileID != nil && executionProfileVersion != nil {
		recipe := projects.ExecutionRecipe{Template: projects.ProfileVersionRef{ID: *executionProfileID, Version: *executionProfileVersion}}
		config.ExecutionRecipe = &recipe
	}
	return &config, nil
}
