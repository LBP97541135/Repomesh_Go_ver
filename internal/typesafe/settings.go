package typesafe

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"repomesh.local/repomesh/internal/secrets"
)

type Service struct {
	pool    *pgxpool.Pool
	secrets *secrets.Store
	client  *Client
}

func New(pool *pgxpool.Pool, store *secrets.Store) *Service {
	return &Service{pool: pool, secrets: store, client: NewClient()}
}

func rollback(tx pgx.Tx) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = tx.Rollback(ctx)
}
func unavailable() error                 { return fail(503, "TYPESAFE_UNAVAILABLE") }
func owner(project string) secrets.Owner { return secrets.Owner{Kind: "typesafe-project", ID: project} }

// Lock the same project row for settings mutations, including the first save.
func projectAccess(ctx context.Context, tx pgx.Tx, actor, project string, lock bool) error {
	query := `SELECT p.id FROM repomesh_projects.projects p JOIN repomesh_access.accounts a ON a.id=p.owner
		WHERE p.id=$1 AND p.owner=$2 AND p.removed_at IS NULL AND NOT a.disabled`
	if lock {
		query += ` FOR UPDATE OF p`
	}
	var id string
	err := tx.QueryRow(ctx, query, project, actor).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return fail(404, "RESOURCE_NOT_FOUND")
	}
	if err != nil {
		return unavailable()
	}
	return nil
}
func readSettings(ctx context.Context, tx pgx.Tx, project string) (Settings, string, error) {
	v := Settings{ProjectID: project, Model: DefaultModel, CheckStatus: "not_checked", SkillCommit: SkillCommit, SkillHash: SkillHash()}
	var secret string
	err := tx.QueryRow(ctx, `SELECT revision,enabled,model,COALESCE(secret_version,''),checked_at,check_status
		FROM repomesh_typesafe.settings WHERE project_id=$1`, project).Scan(&v.Revision, &v.Enabled, &v.Model, &secret, &v.CheckedAt, &v.CheckStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		return v, "", nil
	}
	if err != nil {
		return Settings{}, "", unavailable()
	}
	v.Configured = secret != ""
	if v.CheckStatus == "checking" && v.CheckedAt != nil && time.Since(*v.CheckedAt) > CallTimeout+10*time.Second {
		v.CheckStatus = "TYPESAFE_OUTCOME_UNKNOWN"
	}
	return v, secret, nil
}
func (s *Service) Settings(ctx context.Context, actor, project string) (Settings, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Settings{}, unavailable()
	}
	defer rollback(tx)
	if err = projectAccess(ctx, tx, actor, project, false); err != nil {
		return Settings{}, err
	}
	v, _, err := readSettings(ctx, tx, project)
	return v, err
}

func (s *Service) Save(ctx context.Context, actor, project string, in SaveInput) (Settings, error) {
	if in.ExpectedRevision < 0 || in.Model != DefaultModel {
		return Settings{}, fail(422, "TYPESAFE_INVALID_SETTINGS")
	}
	key := strings.TrimSpace(in.Secret.Value)
	switch in.Secret.Mode {
	case "keep", "clear":
		if in.Secret.Value != "" {
			return Settings{}, fail(422, "TYPESAFE_INVALID_SETTINGS")
		}
	case "replace":
		if len(key) < 8 || len(key) > 4096 || strings.ContainsAny(key, "\r\n\t ") {
			return Settings{}, fail(422, "TYPESAFE_INVALID_KEY")
		}
	default:
		return Settings{}, fail(422, "TYPESAFE_INVALID_SETTINGS")
	}
	if s.secrets == nil {
		return Settings{}, unavailable()
	}
	// Authorization precedes wrapping to avoid unauthorized writes to the vault.
	if _, err := s.Settings(ctx, actor, project); err != nil {
		return Settings{}, err
	}
	var prepared *secrets.PreparedSecret
	if in.Secret.Mode == "replace" {
		plain := []byte(key)
		var err error
		prepared, err = s.secrets.Prepare(ctx, owner(project), secrets.TypeSafeAPIKey, plain)
		clear(plain)
		if err != nil {
			return Settings{}, unavailable()
		}
		defer prepared.Discard()
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Settings{}, unavailable()
	}
	defer rollback(tx)
	if err = projectAccess(ctx, tx, actor, project, true); err != nil {
		return Settings{}, err
	}
	if _, err = tx.Exec(ctx, `SELECT project_id FROM repomesh_typesafe.settings WHERE project_id=$1 FOR UPDATE`, project); err != nil {
		return Settings{}, unavailable()
	}
	old, secret, err := readSettings(ctx, tx, project)
	if err != nil {
		return Settings{}, err
	}
	if old.Revision != in.ExpectedRevision {
		return Settings{}, fail(409, "TYPESAFE_REVISION_CONFLICT")
	}
	if in.Secret.Mode != "keep" && secret != "" {
		if err = s.secrets.DestroyInTx(ctx, tx, secrets.VersionID(secret), owner(project), secrets.TypeSafeAPIKey); err != nil {
			return Settings{}, unavailable()
		}
		secret = ""
	}
	if prepared != nil {
		id, err := s.secrets.InsertPrepared(ctx, tx, prepared)
		if err != nil {
			return Settings{}, unavailable()
		}
		secret = string(id)
	}
	if in.Secret.Mode == "clear" {
		in.Enabled = false
	}
	if in.Enabled && secret == "" {
		return Settings{}, fail(422, "TYPESAFE_KEY_REQUIRED")
	}
	var ref any
	if secret != "" {
		ref = secret
	}
	_, err = tx.Exec(ctx, `INSERT INTO repomesh_typesafe.settings(project_id,revision,enabled,model,secret_version)
		VALUES($1,$2,$3,$4,$5) ON CONFLICT(project_id) DO UPDATE SET
		revision=EXCLUDED.revision,enabled=EXCLUDED.enabled,model=EXCLUDED.model,secret_version=EXCLUDED.secret_version,
		updated_at=clock_timestamp(),check_status='not_checked'`, project, old.Revision+1, in.Enabled, in.Model, ref)
	if err != nil {
		return Settings{}, unavailable()
	}
	v, _, err := readSettings(ctx, tx, project)
	if err != nil {
		return Settings{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Settings{}, unavailable()
	}
	return v, nil
}

func (s *Service) Check(ctx context.Context, actor, project string, revision int64) (Settings, error) {
	if s.secrets == nil {
		return Settings{}, unavailable()
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Settings{}, unavailable()
	}
	defer rollback(tx)
	if err = projectAccess(ctx, tx, actor, project, true); err != nil {
		return Settings{}, err
	}
	v, secret, err := readSettings(ctx, tx, project)
	if err != nil {
		return Settings{}, err
	}
	if v.Revision != revision {
		return Settings{}, fail(409, "TYPESAFE_REVISION_CONFLICT")
	}
	if secret == "" {
		return Settings{}, fail(422, "TYPESAFE_KEY_REQUIRED")
	}
	if v.CheckedAt != nil && time.Since(*v.CheckedAt) < time.Minute {
		return Settings{}, fail(429, "TYPESAFE_CHECK_COOLDOWN")
	}
	key, err := s.secrets.OpenInTx(ctx, tx, secrets.VersionID(secret), owner(project), secrets.TypeSafeAPIKey)
	if err != nil {
		return Settings{}, unavailable()
	}
	defer clear(key)
	_, err = tx.Exec(ctx, `UPDATE repomesh_typesafe.settings SET checked_at=clock_timestamp(),check_status='checking' WHERE project_id=$1`, project)
	if err != nil {
		return Settings{}, unavailable()
	}
	if err = tx.Commit(ctx); err != nil {
		return Settings{}, unavailable()
	}
	_, callErr := s.client.Evaluate(ctx, key, v.Model, Input{Claims: []Claim{{ID: "connection", Text: "The synthetic test exited with code 0."}}, Evidence: "Synthetic test run: exit code 0; one assertion passed."})
	status := "ready"
	if callErr != nil {
		status = errorCode(callErr)
	}
	finish, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	tag, err := s.pool.Exec(finish, `UPDATE repomesh_typesafe.settings SET check_status=$3 WHERE project_id=$1 AND revision=$2`, project, revision, status)
	if err != nil {
		return Settings{}, unavailable()
	}
	if tag.RowsAffected() == 0 {
		return Settings{}, fail(409, "TYPESAFE_REVISION_CONFLICT")
	}
	return s.Settings(finish, actor, project)
}

func errorCode(err error) string {
	var f *Failure
	if errors.As(err, &f) {
		return f.Code
	}
	return "TYPESAFE_UNAVAILABLE"
}
