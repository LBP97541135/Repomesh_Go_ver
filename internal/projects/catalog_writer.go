package projects

import (
	"context"
	"crypto/rand"
	"encoding/hex"

	"github.com/jackc/pgx/v5"
	"repomesh.local/repomesh/internal/secrets"
)

type CatalogWriter struct{}

func NewCatalogWriter() *CatalogWriter { return &CatalogWriter{} }

func (w *CatalogWriter) LockExclusive(ctx context.Context, tx pgx.Tx) error {
	var singleton bool
	if err := tx.QueryRow(ctx, `SELECT singleton FROM repomesh_projects.catalog WHERE singleton FOR UPDATE`).Scan(&singleton); err != nil || !singleton {
		return unavailable()
	}
	return nil
}

type ModelVersionRegistration struct {
	Owner              string
	ProfileID          string
	Name               string
	ParametersComplete bool
	ProviderID         string
	ProviderRevision   string
	ModelRowID         string
	SecretVersion      secrets.VersionID
}

type ExecutionVersionRegistration struct {
	Owner                    string
	ProfileID                string
	Version                  string
	Name                     string
	WorkerConcurrency        int
	VerificationGroupEnabled bool
}

func (w *CatalogWriter) RegisterModelVersion(ctx context.Context, tx pgx.Tx, value ModelVersionRegistration) error {
	if value.Owner == "" || value.ProfileID == "" || value.Name == "" || value.ProviderRevision == "" || value.SecretVersion == "" {
		return unavailable()
	}
	_, err := tx.Exec(ctx, `INSERT INTO repomesh_projects.profiles(kind,id,owner,name,enabled,current_version)
		VALUES ('model',$1,$2,$3,true,$4)
		ON CONFLICT (kind,id) DO UPDATE SET current_version=EXCLUDED.current_version,name=EXCLUDED.name
		WHERE repomesh_projects.profiles.owner=EXCLUDED.owner`, value.ProfileID, value.Owner, value.Name, value.ProviderRevision)
	if err != nil {
		return unavailable()
	}
	_, err = tx.Exec(ctx, `INSERT INTO repomesh_projects.profile_versions(kind,profile_id,version,secret_version_id,secret_owner_kind,secret_owner_id,secret_purpose,parameters_complete)
		VALUES ('model',$1,$2,$3,'model-provider',$4,'model-provider-key',$5)`,
		value.ProfileID, value.ProviderRevision, string(value.SecretVersion), value.ProviderID, value.ParametersComplete)
	if err != nil {
		return unavailable()
	}
	return nil
}

// The head moves only when a version row is inserted here, so an import that
// re-lists an older version cannot move current_version backwards.
func (w *CatalogWriter) RegisterExecutionVersion(ctx context.Context, tx pgx.Tx, value ExecutionVersionRegistration) error {
	if value.Owner == "" || value.ProfileID == "" || value.Version == "" || value.Name == "" || value.WorkerConcurrency < 1 || value.WorkerConcurrency > 16 {
		return unavailable()
	}
	_, err := tx.Exec(ctx, `INSERT INTO repomesh_projects.profiles(kind,id,owner,name,enabled,current_version)
		VALUES ('execution',$1,$2,$3,true,$4)
		ON CONFLICT (kind,id) DO NOTHING`, value.ProfileID, value.Owner, value.Name, value.Version)
	if err != nil {
		return unavailable()
	}
	var owner string
	if err = tx.QueryRow(ctx, `SELECT owner FROM repomesh_projects.profiles WHERE kind='execution' AND id=$1 FOR UPDATE`, value.ProfileID).Scan(&owner); err != nil {
		return unavailable()
	}
	if owner != value.Owner {
		return failure(409, "PROFILE_OWNER_CONFLICT")
	}
	inserted, err := tx.Exec(ctx, `INSERT INTO repomesh_projects.profile_versions(kind,profile_id,version,parameters_complete,worker_concurrency,verification_group_enabled)
		VALUES ('execution',$1,$2,false,$3,$4)
		ON CONFLICT (kind,profile_id,version) DO NOTHING`, value.ProfileID, value.Version, value.WorkerConcurrency, value.VerificationGroupEnabled)
	if err != nil {
		return unavailable()
	}
	if inserted.RowsAffected() == 0 {
		return nil
	}
	if _, err = tx.Exec(ctx, `UPDATE repomesh_projects.profiles SET current_version=$2,name=$3 WHERE kind='execution' AND id=$1`, value.ProfileID, value.Version, value.Name); err != nil {
		return unavailable()
	}
	return nil
}

func (w *CatalogWriter) BindExecutionDefault(ctx context.Context, tx pgx.Tx, owner, profileID, version string) (string, error) {
	if owner == "" || profileID == "" || version == "" {
		return "", unavailable()
	}
	revision := newCatalogID()
	_, err := tx.Exec(ctx, `INSERT INTO repomesh_projects.defaults(actor,kind,profile_id,default_revision,pinned_version)
		VALUES ($1,'execution',$2,$3,$4)
		ON CONFLICT (actor,kind) DO UPDATE SET profile_id=EXCLUDED.profile_id,default_revision=EXCLUDED.default_revision,pinned_version=EXCLUDED.pinned_version`,
		owner, profileID, revision, version)
	if err != nil {
		return "", unavailable()
	}
	return revision, nil
}

func newCatalogID() string {
	var value [16]byte
	_, _ = rand.Read(value[:])
	value[6] = value[6]&15 | 64
	value[8] = value[8]&63 | 128
	return hex.EncodeToString(value[:4]) + "-" + hex.EncodeToString(value[4:6]) + "-" + hex.EncodeToString(value[6:8]) + "-" + hex.EncodeToString(value[8:10]) + "-" + hex.EncodeToString(value[10:])
}

// RegisterCompleteExecution registers one complete (schema2) execution version
// on the projects side: the profile head plus the pinned policy ids in
// profile_versions. The sources-side execution_versions row (complete=true
// with its recipe columns) is written by the sources importer itself, so this
// method never touches repomesh_sources.
func (w *CatalogWriter) RegisterCompleteExecution(ctx context.Context, tx pgx.Tx, owner, profileID, version, name string, recipe ExecutionRecipe) error {
	if owner == "" || profileID == "" || version == "" || name == "" ||
		recipe.WorkerConcurrency < 1 || recipe.WorkerConcurrency > 16 ||
		recipe.Budget.Ref.ID == "" || recipe.Budget.Ref.Version == "" ||
		recipe.Limits.Ref.ID == "" || recipe.Limits.Ref.Version == "" {
		return unavailable()
	}
	_, err := tx.Exec(ctx, `INSERT INTO repomesh_projects.profiles(kind,id,owner,name,enabled,current_version)
		VALUES ('execution',$1,$2,$3,true,$4)
		ON CONFLICT (kind,id) DO NOTHING`, profileID, owner, name, version)
	if err != nil {
		return unavailable()
	}
	var existingOwner string
	if err = tx.QueryRow(ctx, `SELECT owner FROM repomesh_projects.profiles WHERE kind='execution' AND id=$1 FOR UPDATE`, profileID).Scan(&existingOwner); err != nil {
		return unavailable()
	}
	if existingOwner != owner {
		return failure(409, "PROFILE_OWNER_CONFLICT")
	}
	inserted, err := tx.Exec(ctx, `INSERT INTO repomesh_projects.profile_versions(kind,profile_id,version,parameters_complete,worker_concurrency,verification_group_enabled,budget_policy_id,time_limit_policy_id)
		VALUES ('execution',$1,$2,false,$3,$4,$5,$6)
		ON CONFLICT (kind,profile_id,version) DO NOTHING`,
		profileID, version, recipe.WorkerConcurrency, recipe.VerificationGroupEnabled,
		recipe.Budget.Ref.ID, recipe.Limits.Ref.ID)
	if err != nil {
		return unavailable()
	}
	if inserted.RowsAffected() == 0 {
		return nil
	}
	if _, err = tx.Exec(ctx, `UPDATE repomesh_projects.profiles SET current_version=$2,name=$3 WHERE kind='execution' AND id=$1`, profileID, version, name); err != nil {
		return unavailable()
	}
	return nil
}

// RegisterRequestPolicy writes one immutable request policy version.
func (w *CatalogWriter) RegisterRequestPolicy(ctx context.Context, tx pgx.Tx, policy RequestPolicy) error {
	if policy.Ref.ID == "" || policy.Ref.Version == "" ||
		policy.Scope != "actor_model_test" && policy.Scope != "project_model_runtime" ||
		policy.Limit <= 0 || policy.MaxUnresolved <= 0 {
		return unavailable()
	}
	_, err := tx.Exec(ctx, `INSERT INTO repomesh_projects.request_policy_versions(id,version,scope,daily_limit,max_unresolved,enabled)
		VALUES ($1,$2,$3,$4,$5,$6)`,
		policy.Ref.ID, policy.Ref.Version, policy.Scope, policy.Limit, policy.MaxUnresolved, policy.Enabled)
	if err != nil {
		return unavailable()
	}
	return nil
}

// RegisterTimePolicy writes one immutable time policy version.
func (w *CatalogWriter) RegisterTimePolicy(ctx context.Context, tx pgx.Tx, policy TimePolicy) error {
	if policy.Ref.ID == "" || policy.Ref.Version == "" ||
		policy.ModelRequestSeconds <= 0 || policy.WorkerAttemptSeconds <= 0 {
		return unavailable()
	}
	_, err := tx.Exec(ctx, `INSERT INTO repomesh_projects.time_policy_versions(id,version,scope,model_request_seconds,worker_attempt_seconds)
		VALUES ($1,$2,'project_model_runtime',$3,$4)`,
		policy.Ref.ID, policy.Ref.Version, policy.ModelRequestSeconds, policy.WorkerAttemptSeconds)
	if err != nil {
		return unavailable()
	}
	return nil
}
