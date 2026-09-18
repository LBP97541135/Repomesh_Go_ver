package sources

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"repomesh.local/repomesh/internal/projects"
)

func AuthenticateDeployment(ctx context.Context, pool *pgxpool.Pool) (DeploymentPrincipal, error) {
	if pool == nil {
		return DeploymentPrincipal{}, unavailable()
	}
	var sessionUser, currentUser string
	var permitted bool
	if err := pool.QueryRow(ctx, `SELECT session_user, current_user, pg_has_role(current_user, 'repomesh_source_importer', 'MEMBER')`).Scan(&sessionUser, &currentUser, &permitted); err != nil {
		return DeploymentPrincipal{}, unavailable()
	}
	if !permitted || sessionUser == "" || currentUser != sessionUser {
		return DeploymentPrincipal{}, failure(2, "DEPLOYMENT_FORBIDDEN")
	}
	var deploymentID string
	if err := pool.QueryRow(ctx, `SELECT deployment_id FROM repomesh_sources.deployment WHERE singleton`).Scan(&deploymentID); err != nil {
		return DeploymentPrincipal{}, unavailable()
	}
	return DeploymentPrincipal{deploymentID: deploymentID, sessionUser: sessionUser}, nil
}

func checkDeployment(ctx context.Context, tx pgx.Tx, principal DeploymentPrincipal) error {
	var sessionUser string
	var permitted bool
	if err := tx.QueryRow(ctx, `SELECT session_user, pg_has_role(current_user, 'repomesh_source_importer', 'MEMBER')`).Scan(&sessionUser, &permitted); err != nil || !permitted || sessionUser != principal.sessionUser {
		return failure(2, "DEPLOYMENT_FORBIDDEN")
	}
	var stored string
	if err := tx.QueryRow(ctx, `SELECT deployment_id FROM repomesh_sources.deployment WHERE singleton`).Scan(&stored); err != nil || stored != principal.deploymentID {
		return unavailable()
	}
	return nil
}

func NewImporter(pool *pgxpool.Pool, principal DeploymentPrincipal, catalog *projects.CatalogWriter) (*Importer, error) {
	if pool == nil || catalog == nil || principal.deploymentID == "" || principal.sessionUser == "" {
		return nil, unavailable()
	}
	return &Importer{pool: pool, principal: principal, catalog: catalog}, nil
}

func (s *Importer) phase(ctx context.Context, phase importPhase) error {
	if s.hook == nil {
		return nil
	}
	return s.hook(ctx, phase)
}

func (s *Importer) Import(ctx context.Context, command ImportCommand) (ImportResult, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return ImportResult{}, unavailable()
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(ctx, `SET LOCAL lock_timeout='5s'`); err != nil {
		return ImportResult{}, unavailable()
	}
	if _, err = tx.Exec(ctx, `SET LOCAL statement_timeout='15s'`); err != nil {
		return ImportResult{}, unavailable()
	}
	if _, err = tx.Exec(ctx, `SET LOCAL synchronous_commit=on`); err != nil {
		return ImportResult{}, unavailable()
	}
	if err = checkDeployment(ctx, tx, s.principal); err != nil {
		return ImportResult{}, err
	}
	if err = lockImport(ctx, tx, s.principal.deploymentID, commandImportID(command)); err != nil {
		return ImportResult{}, err
	}
	if err = s.phase(ctx, importLocked); err != nil {
		return ImportResult{}, err
	}
	prior, canonical, found, err := readImport(ctx, tx, s.principal.deploymentID, commandImportID(command))
	if err != nil {
		return ImportResult{}, err
	}
	if found {
		if !bytes.Equal(canonical, command.canonical) {
			return ImportResult{}, failure(2, "IMPORT_CONFLICT")
		}
		if err = tx.Commit(ctx); err != nil {
			return ImportResult{}, unavailable()
		}
		return ImportResult{Receipt: prior, Replayed: true}, nil
	}
	owners := ownerSetFor(command)
	if err = lockOwners(ctx, tx, owners); err != nil {
		return ImportResult{}, err
	}
	if err = s.phase(ctx, ownersLocked); err != nil {
		return ImportResult{}, err
	}
	if err = s.catalog.LockExclusive(ctx, tx); err != nil {
		return ImportResult{}, err
	}
	var at time.Time
	if err = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&at); err != nil {
		return ImportResult{}, unavailable()
	}
	var receipt Receipt
	switch payload := command.payload.(type) {
	case Manifest:
		receipt, err = s.importV1(ctx, tx, payload, at)
	case ManifestV2:
		receipt, err = s.importV2(ctx, tx, at, payload)
	default:
		err = unavailable()
	}
	if err != nil {
		return ImportResult{}, err
	}
	if err = s.phase(ctx, defaultsBound); err != nil {
		return ImportResult{}, err
	}
	if err = commitImport(ctx, tx, s.principal.deploymentID, command, receipt); err != nil {
		return ImportResult{}, err
	}
	if err = s.phase(ctx, importReceiptInserted); err != nil {
		return ImportResult{}, err
	}
	if err = s.phase(ctx, importBeforeCommit); err != nil {
		return ImportResult{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return ImportResult{}, unavailable()
	}
	return ImportResult{Receipt: receipt}, nil
}

func (s *Importer) Get(ctx context.Context, importID string) (Receipt, error) {
	importID = strings.ToLower(importID)
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Receipt{}, unavailable()
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = checkDeployment(ctx, tx, s.principal); err != nil {
		return Receipt{}, err
	}
	receipt, _, found, err := readImport(ctx, tx, s.principal.deploymentID, importID)
	if err != nil {
		return Receipt{}, err
	}
	if !found {
		return Receipt{}, failure(2, "IMPORT_NOT_FOUND")
	}
	if err = tx.Commit(ctx); err != nil {
		return Receipt{}, unavailable()
	}
	return receipt, nil
}

func lockImport(ctx context.Context, tx pgx.Tx, _, _ string) error {
	var singleton bool
	if err := tx.QueryRow(ctx, `SELECT singleton FROM repomesh_sources.import_serialization WHERE singleton FOR UPDATE`).Scan(&singleton); err != nil || !singleton {
		return unavailable()
	}
	return nil
}

func lockOwners(ctx context.Context, tx pgx.Tx, owners []string) error {
	sort.Strings(owners)
	for _, owner := range owners {
		var id string
		if err := tx.QueryRow(ctx, `SELECT id FROM repomesh_access.accounts WHERE id=$1 FOR UPDATE`, owner).Scan(&id); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return failure(2, "UNKNOWN_OWNER")
			}
			return unavailable()
		}
	}
	return nil
}

func readImport(ctx context.Context, tx pgx.Tx, deploymentID, importID string) (Receipt, []byte, bool, error) {
	var raw []byte
	var canonical []byte
	err := tx.QueryRow(ctx, `SELECT receipt,canonical FROM repomesh_sources.imports WHERE deployment_id=$1 AND import_id=$2`, deploymentID, importID).Scan(&raw, &canonical)
	if errors.Is(err, pgx.ErrNoRows) {
		return Receipt{}, nil, false, nil
	}
	if err != nil {
		return Receipt{}, nil, false, unavailable()
	}
	var receipt Receipt
	if json.Unmarshal(raw, &receipt) != nil {
		return Receipt{}, nil, false, unavailable()
	}
	if receipt.SchemaVersion == 2 {
		var extras struct {
			BudgetPolicies    []VersionRef        `json:"budgetPolicies"`
			TimeLimitPolicies []VersionRef        `json:"timeLimitPolicies"`
			EgressPolicies    []VersionRef        `json:"egressPolicies"`
			TestBindings      []TestBindingResult `json:"testBindings"`
		}
		if json.Unmarshal(raw, &extras) != nil {
			return Receipt{}, nil, false, unavailable()
		}
		receipt.policyResults = &PolicyImportResults{
			BudgetPolicies: extras.BudgetPolicies, TimeLimitPolicies: extras.TimeLimitPolicies,
			EgressPolicies: extras.EgressPolicies, TestBindings: extras.TestBindings,
		}
	}
	return receipt, canonical, true, nil
}

func insertTemplate(ctx context.Context, tx pgx.Tx, value EnvironmentTemplate) error {
	var existing EnvironmentTemplate
	err := tx.QueryRow(ctx, `SELECT id,version,executor_pool_id,approved_template_digest,network_policy_ref,resource_class_id,enabled
		FROM repomesh_sources.environment_templates WHERE id=$1 AND version=$2`, value.ID, value.Version).Scan(
		&existing.ID, &existing.Version, &existing.ExecutorPoolID, &existing.ApprovedTemplateDigest, &existing.NetworkPolicyRef, &existing.ResourceClassID, &existing.Enabled)
	if errors.Is(err, pgx.ErrNoRows) {
		_, err = tx.Exec(ctx, `INSERT INTO repomesh_sources.environment_templates(id,version,executor_pool_id,approved_template_digest,network_policy_ref,resource_class_id,enabled)
			VALUES ($1,$2,$3,$4,$5,$6,$7)`, value.ID, value.Version, value.ExecutorPoolID, value.ApprovedTemplateDigest, value.NetworkPolicyRef, value.ResourceClassID, value.Enabled)
		if err != nil {
			return unavailable()
		}
		return nil
	}
	if err != nil {
		return unavailable()
	}
	if existing != value {
		return failure(2, "TEMPLATE_CONFLICT")
	}
	return nil
}

func insertExecutionSource(ctx context.Context, tx pgx.Tx, value ExecutionProfile) error {
	var existing ExecutionProfile
	err := tx.QueryRow(ctx, `SELECT profile_id,version,owner,name,template_id,template_version,worker_concurrency,verification_group_enabled
		FROM repomesh_sources.execution_versions WHERE profile_id=$1 AND version=$2`, value.ID, value.Version).Scan(
		&existing.ID, &existing.Version, &existing.OwnerID, &existing.Name, &existing.TemplateID, &existing.TemplateVersion, &existing.WorkerConcurrency, &existing.VerificationGroupEnabled)
	if errors.Is(err, pgx.ErrNoRows) {
		_, err = tx.Exec(ctx, `INSERT INTO repomesh_sources.execution_versions(profile_id,version,owner,name,template_id,template_version,worker_concurrency,verification_group_enabled)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`, value.ID, value.Version, value.OwnerID, value.Name, value.TemplateID, value.TemplateVersion, value.WorkerConcurrency, value.VerificationGroupEnabled)
		if err != nil {
			return unavailable()
		}
		return nil
	}
	if err != nil {
		return unavailable()
	}
	if existing != value {
		return failure(2, "EXECUTION_CONFLICT")
	}
	return nil
}

func commitImport(ctx context.Context, tx pgx.Tx, deploymentID string, command ImportCommand, receipt Receipt) error {
	body, err := json.Marshal(receipt)
	if err != nil {
		return unavailable()
	}
	_, err = tx.Exec(ctx, `INSERT INTO repomesh_sources.imports(deployment_id,import_id,schema_version,canonical,receipt,committed_at)
		VALUES ($1,$2,$3,$4,$5,$6)`, deploymentID, commandImportID(command), command.schemaVersion, command.canonical, body, receipt.CommittedAt)
	if err != nil {
		return unavailable()
	}
	return nil
}

func ownerSet(manifest Manifest) []string {
	seen := map[string]bool{}
	for _, profile := range manifest.ExecutionProfiles {
		seen[profile.OwnerID] = true
	}
	for _, binding := range manifest.DefaultBindings {
		seen[binding.OwnerID] = true
	}
	owners := make([]string, 0, len(seen))
	for owner := range seen {
		owners = append(owners, owner)
	}
	return owners
}
