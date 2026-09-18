package sources

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"time"

	"github.com/jackc/pgx/v5"
	"repomesh.local/repomesh/internal/projects"
)

// v2 helpers: policy registration, complete execution rows and test bindings.

func insertEgressPolicy(ctx context.Context, tx pgx.Tx, value EgressPolicy) error {
	var urls []string
	var port int
	var private, redirects bool
	err := tx.QueryRow(ctx, `SELECT approved_base_urls,allowed_port,allow_private_addresses,follow_redirects
		FROM repomesh_sources.egress_policy_versions WHERE id=$1 AND version=$2`, value.ID, value.Version).Scan(&urls, &port, &private, &redirects)
	if err == pgx.ErrNoRows {
		_, err = tx.Exec(ctx, `INSERT INTO repomesh_sources.egress_policy_versions(id,version,approved_base_urls,allowed_port,allow_private_addresses,follow_redirects)
			VALUES ($1,$2,$3,$4,$5,$6)`, value.ID, value.Version, value.ApprovedBaseURLs, value.AllowedPort, value.AllowPrivateAddresses, value.FollowRedirects)
		if err != nil {
			return unavailable()
		}
		return nil
	}
	if err != nil {
		return unavailable()
	}
	if len(urls) != len(value.ApprovedBaseURLs) || port != value.AllowedPort || private != value.AllowPrivateAddresses || redirects != value.FollowRedirects {
		return failure(2, "EGRESS_CONFLICT")
	}
	for i := range urls {
		if urls[i] != value.ApprovedBaseURLs[i] {
			return failure(2, "EGRESS_CONFLICT")
		}
	}
	return nil
}

func bindTestPolicy(ctx context.Context, tx pgx.Tx, binding TestBinding) (string, error) {
	revision := newSourceRevision()
	_, err := tx.Exec(ctx, `INSERT INTO repomesh_sources.test_bindings(owner,budget_policy_id,budget_policy_version,time_limit_policy_id,time_limit_policy_version,egress_policy_id,egress_policy_version,combination_revision)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		ON CONFLICT (owner) DO UPDATE SET budget_policy_id=EXCLUDED.budget_policy_id,budget_policy_version=EXCLUDED.budget_policy_version,time_limit_policy_id=EXCLUDED.time_limit_policy_id,time_limit_policy_version=EXCLUDED.time_limit_policy_version,egress_policy_id=EXCLUDED.egress_policy_id,egress_policy_version=EXCLUDED.egress_policy_version,combination_revision=EXCLUDED.combination_revision`,
		binding.OwnerID, binding.Budget.ID, binding.Budget.Version, binding.Limits.ID, binding.Limits.Version, binding.Egress.ID, binding.Egress.Version, revision)
	if err != nil {
		return "", unavailable()
	}
	return revision, nil
}

// insertCompleteExecution writes the sources-side complete (schema2) execution
// version row. SELECT-then-INSERT is idempotent; any difference is a conflict.
func insertCompleteExecution(ctx context.Context, tx pgx.Tx, execution CompleteExecution) error {
	var existing ExecutionProfile
	var budgetID, budgetVersion, limitsID, limitsVersion string
	var complete bool
	err := tx.QueryRow(ctx, `SELECT profile_id,version,owner,name,template_id,template_version,worker_concurrency,verification_group_enabled,budget_policy_id,budget_policy_version,time_limit_policy_id,time_limit_policy_version,complete
		FROM repomesh_sources.execution_versions WHERE profile_id=$1 AND version=$2`,
		execution.Identity.ID, execution.Identity.Version).Scan(
		&existing.ID, &existing.Version, &existing.OwnerID, &existing.Name, &existing.TemplateID, &existing.TemplateVersion, &existing.WorkerConcurrency, &existing.VerificationGroupEnabled,
		&budgetID, &budgetVersion, &limitsID, &limitsVersion, &complete)
	if err == pgx.ErrNoRows {
		_, err = tx.Exec(ctx, `INSERT INTO repomesh_sources.execution_versions(profile_id,version,owner,name,template_id,template_version,worker_concurrency,verification_group_enabled,budget_policy_id,budget_policy_version,time_limit_policy_id,time_limit_policy_version,complete)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,true)`,
			execution.Identity.ID, execution.Identity.Version, execution.Identity.OwnerID, execution.Identity.Name, execution.Identity.TemplateID, execution.Identity.TemplateVersion, execution.Identity.WorkerConcurrency, execution.Identity.VerificationGroupEnabled,
			execution.Budget.ID, execution.Budget.Version, execution.Limits.ID, execution.Limits.Version)
		if err != nil {
			return unavailable()
		}
		return nil
	}
	if err != nil {
		return unavailable()
	}
	if existing != execution.Identity || budgetID != execution.Budget.ID || budgetVersion != execution.Budget.Version || limitsID != execution.Limits.ID || limitsVersion != execution.Limits.Version || !complete {
		return failure(2, "EXECUTION_CONFLICT")
	}
	return nil
}

// importV2 runs the schema2 flow inside the caller's transaction. The lock
// order matches schema1: import singleton, owner accounts, catalog exclusive.
func (s *Importer) importV2(ctx context.Context, tx pgx.Tx, at time.Time, manifest ManifestV2) (Receipt, error) {
	receipt := Receipt{ImportID: manifest.Common.ImportID, SchemaVersion: 2, CommittedAt: time.Time{}}
	results := PolicyImportResults{}
	for _, template := range manifest.Common.EnvironmentTemplates {
		if err := insertTemplate(ctx, tx, template); err != nil {
			return Receipt{}, err
		}
		receipt.EnvironmentTemplates = append(receipt.EnvironmentTemplates, VersionRef{ID: template.ID, Version: template.Version})
	}
	for _, budget := range manifest.Budgets {
		if err := s.catalog.RegisterRequestPolicy(ctx, tx, budget); err != nil {
			return Receipt{}, err
		}
		results.BudgetPolicies = append(results.BudgetPolicies, VersionRef{ID: budget.Ref.ID, Version: budget.Ref.Version})
	}
	for _, limits := range manifest.Limits {
		if err := s.catalog.RegisterTimePolicy(ctx, tx, limits); err != nil {
			return Receipt{}, err
		}
		results.TimeLimitPolicies = append(results.TimeLimitPolicies, VersionRef{ID: limits.Ref.ID, Version: limits.Ref.Version})
	}
	for _, egress := range manifest.Egress {
		if err := insertEgressPolicy(ctx, tx, egress); err != nil {
			return Receipt{}, err
		}
		results.EgressPolicies = append(results.EgressPolicies, VersionRef{ID: egress.ID, Version: egress.Version})
	}
	for _, execution := range manifest.ExecutionProfiles {
		recipe := projects.ExecutionRecipe{
			Template:                 projects.ProfileVersionRef{ID: execution.Identity.TemplateID, Version: execution.Identity.TemplateVersion},
			WorkerConcurrency:        execution.Identity.WorkerConcurrency,
			VerificationGroupEnabled: execution.Identity.VerificationGroupEnabled,
			Budget:                   projects.RequestPolicy{Ref: execution.Budget},
			Limits:                   projects.TimePolicy{Ref: execution.Limits},
		}
		if err := s.catalog.RegisterCompleteExecution(ctx, tx, execution.Identity.OwnerID, execution.Identity.ID, execution.Identity.Version, execution.Identity.Name, recipe); err != nil {
			return Receipt{}, err
		}
		if err := insertCompleteExecution(ctx, tx, execution); err != nil {
			return Receipt{}, err
		}
		receipt.ExecutionProfiles = append(receipt.ExecutionProfiles, VersionRef{ID: execution.Identity.ID, Version: execution.Identity.Version})
	}
	for _, binding := range manifest.Tests {
		revision, err := bindTestPolicy(ctx, tx, binding)
		if err != nil {
			return Receipt{}, err
		}
		results.TestBindings = append(results.TestBindings, TestBindingResult{OwnerID: binding.OwnerID, Revision: revision})
	}
	for _, binding := range manifest.Common.DefaultBindings {
		revision, err := s.catalog.BindExecutionDefault(ctx, tx, binding.OwnerID, binding.ProfileID, binding.ProfileVersion)
		if err != nil {
			return Receipt{}, err
		}
		receipt.DefaultBindings = append(receipt.DefaultBindings, DefaultResult{OwnerID: binding.OwnerID, ProfileID: binding.ProfileID, ProfileVersion: binding.ProfileVersion, DefaultRevision: revision})
	}
	receipt.policyResults = &results
	return receipt, nil
}

// importV1 is the schema1 flow extracted verbatim from Import.
func (s *Importer) importV1(ctx context.Context, tx pgx.Tx, manifest Manifest, at time.Time) (Receipt, error) {
	receipt := Receipt{ImportID: manifest.ImportID, SchemaVersion: 1, CommittedAt: at}
	for _, template := range manifest.EnvironmentTemplates {
		if err := insertTemplate(ctx, tx, template); err != nil {
			return Receipt{}, err
		}
		receipt.EnvironmentTemplates = append(receipt.EnvironmentTemplates, VersionRef{ID: template.ID, Version: template.Version})
	}
	if err := s.phase(ctx, templatesInserted); err != nil {
		return Receipt{}, err
	}
	for _, profile := range manifest.ExecutionProfiles {
		if err := s.catalog.RegisterExecutionVersion(ctx, tx, projects.ExecutionVersionRegistration{
			Owner: profile.OwnerID, ProfileID: profile.ID, Version: profile.Version, Name: profile.Name,
			WorkerConcurrency: profile.WorkerConcurrency, VerificationGroupEnabled: profile.VerificationGroupEnabled,
		}); err != nil {
			return Receipt{}, err
		}
		if err := insertExecutionSource(ctx, tx, profile); err != nil {
			return Receipt{}, err
		}
		receipt.ExecutionProfiles = append(receipt.ExecutionProfiles, VersionRef{ID: profile.ID, Version: profile.Version})
	}
	if err := s.phase(ctx, executionProfilesInserted); err != nil {
		return Receipt{}, err
	}
	for _, binding := range manifest.DefaultBindings {
		revision, err := s.catalog.BindExecutionDefault(ctx, tx, binding.OwnerID, binding.ProfileID, binding.ProfileVersion)
		if err != nil {
			return Receipt{}, err
		}
		receipt.DefaultBindings = append(receipt.DefaultBindings, DefaultResult{
			OwnerID: binding.OwnerID, ProfileID: binding.ProfileID, ProfileVersion: binding.ProfileVersion, DefaultRevision: revision,
		})
	}
	return receipt, nil
}

// commandImportID and ownerSetFor dispatch over the payload version. The v2
// owner set adds test binding owners so their account locks stay in order.
func commandImportID(command ImportCommand) string {
	switch payload := command.payload.(type) {
	case Manifest:
		return payload.ImportID
	case ManifestV2:
		return payload.Common.ImportID
	default:
		return ""
	}
}

func ownerSetFor(command ImportCommand) []string {
	seen := map[string]bool{}
	switch payload := command.payload.(type) {
	case Manifest:
		for _, profile := range payload.ExecutionProfiles {
			seen[profile.OwnerID] = true
		}
		for _, binding := range payload.DefaultBindings {
			seen[binding.OwnerID] = true
		}
	case ManifestV2:
		for _, profile := range payload.ExecutionProfiles {
			seen[profile.Identity.OwnerID] = true
		}
		for _, binding := range payload.Tests {
			seen[binding.OwnerID] = true
		}
		for _, binding := range payload.Common.DefaultBindings {
			seen[binding.OwnerID] = true
		}
	}
	owners := make([]string, 0, len(seen))
	for owner := range seen {
		owners = append(owners, owner)
	}
	return owners
}

// newSourceRevision builds a uuid-v4 combination revision for test bindings.
func newSourceRevision() string {
	var value [16]byte
	_, _ = rand.Read(value[:])
	value[6] = value[6]&15 | 64
	value[8] = value[8]&63 | 128
	return hex.EncodeToString(value[:4]) + "-" + hex.EncodeToString(value[4:6]) + "-" + hex.EncodeToString(value[6:8]) + "-" + hex.EncodeToString(value[8:10]) + "-" + hex.EncodeToString(value[10:])
}
