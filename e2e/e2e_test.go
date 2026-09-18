//go:build e2e

// E2E full-pipeline test against a live server (DB + HTTP). Build:
//
//	go test -tags=e2e ./e2e/ -run TestFullPipeline -v
//
// Env: E2E_DATABASE_URL, E2E_BASE_URL (default http://127.0.0.1:8080),
//
//	E2E_ORG_ID (optional; defaults to a fresh test org).
//
// Covers: migration status, session-free service chains (org assembly, DAG
// submit→promote→dispatch→approve, dual dispatch, branch validation with the
// local provider, interface-doc multi-approval, delivery ports, observability
// queries) plus idempotent-replay and unauthorized-access refusal checks.
package e2e

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type fixture struct {
	t              *testing.T
	ctx            context.Context
	pool           *pgxpool.Pool
	base           string
	org            string
	orgUUID        string
	ownerID        string
	projectID      string
	configRevision string
}

func mustEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func setup(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{t: t, ctx: context.Background(), base: mustEnv("E2E_BASE_URL", "http://127.0.0.1:8080")}
	pool, err := pgxpool.New(f.ctx, mustEnv("E2E_DATABASE_URL", "postgres://repomesh:repomesh_pg_2026_x7k9@127.0.0.1:5432/repomesh?sslmode=disable"))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	f.pool = pool
	t.Cleanup(pool.Close)
	f.org = mustEnv("E2E_ORG_ID", fmt.Sprintf("e2e%012d", time.Now().UnixNano()%1e12))
	// uuid columns require uuid-shaped ids: derive a deterministic v4-style id
	// from the org tag so reruns address the same org.
	sum := sha256.Sum256([]byte(f.org))
	hexSum := hex.EncodeToString(sum[:])
	f.orgUUID = fmt.Sprintf("%s-%s-4%s-8%s-%s",
		hexSum[0:8], hexSum[8:12], hexSum[13:16], hexSum[17:20], hexSum[20:32])
	f.ownerID = "usr_e2e_" + hexSum[0:12]
	// repomesh_projects.projects pins id and every revision to UUID v4 shape.
	f.projectID = fmt.Sprintf("%s-%s-4%s-8%s-%s",
		hexSum[0:8], hexSum[8:12], hexSum[13:16], hexSum[17:20], hexSum[20:32])
	return f
}

// seedProject creates the minimal business rows the pipeline requires.
func (f *fixture) seedProject() (projectID, issueID string) {
	f.t.Helper()
	if _, err := f.pool.Exec(f.ctx, `
		INSERT INTO repomesh_access.accounts (id, github_id, display_name)
		VALUES ($1, floor(random()*900000000)::bigint + 100000000, 'e2e-owner')
		ON CONFLICT (id) DO NOTHING`, f.ownerID); err != nil {
		f.t.Fatalf("seed account: %v", err)
	}
	configRevision := f.uuid4(3)
	f.configRevision = configRevision
	tx, err := f.pool.Begin(f.ctx)
	if err != nil {
		f.t.Fatalf("seed tx: %v", err)
	}
	// project_current_configuration_fk is deferred: the project may reference
	// the configuration revision inserted later in this same transaction.
	if _, err := tx.Exec(f.ctx, `
		INSERT INTO public.organizations (id, name)
		VALUES ($1, 'e2e-org')
		ON CONFLICT (id) DO NOTHING`, f.orgUUID); err != nil {
		_ = tx.Rollback(f.ctx)
		f.t.Fatalf("seed organization: %v", err)
	}
	if _, err := tx.Exec(f.ctx, `
		INSERT INTO repomesh_projects.projects
			(id, owner, organization_id, name, purpose, revision, creation_context_revision, current_configuration_revision)
		VALUES ($1, $2, $3, 'e2e-project', 'e2e purpose', $4, $5, $6)
		ON CONFLICT (id) DO NOTHING`,
		f.projectID, f.ownerID, f.orgUUID, f.uuid4(1), f.uuid4(2), configRevision); err != nil {
		_ = tx.Rollback(f.ctx)
		f.t.Fatalf("seed project: %v", err)
	}
	if _, err := tx.Exec(f.ctx, `
		INSERT INTO repomesh_projects.configuration_revisions
			(project_id, revision, fixed, created_by, created_at)
		VALUES ($1, $2, '{"e2e":true}'::jsonb, $3, clock_timestamp())
		ON CONFLICT (project_id, revision) DO NOTHING`,
		f.projectID, configRevision, f.ownerID); err != nil {
		_ = tx.Rollback(f.ctx)
		f.t.Fatalf("seed configuration revision: %v", err)
	}
	if err := tx.Commit(f.ctx); err != nil {
		f.t.Fatalf("seed commit: %v", err)
	}
	projectID = f.projectID
	issueID = "iss_e2e_" + fmt.Sprintf("%d", time.Now().UnixNano()%1e9)
	operationID := "opr_e2e_" + fmt.Sprintf("%d", time.Now().UnixNano()%1e9)
	conversationID := "conv_e2e_" + fmt.Sprintf("%d", time.Now().UnixNano()%1e9)
	changeSetID := "cs_e2e_" + fmt.Sprintf("%d", time.Now().UnixNano()%1e9)
	// Number comes from the B06 counter row. The aggregate stays an
	// uncommitted placeholder: the creation operation keeps issue_id NULL so
	// creation_aggregate_complete never engages (this is test data, not a
	// committed creation).
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO repomesh_issues.project_issue_counters (project_id, next_number)
		VALUES ($1, 2) ON CONFLICT (project_id) DO UPDATE
		SET next_number = repomesh_issues.project_issue_counters.next_number + 1`,
		projectID); err != nil {
		f.t.Fatalf("seed counter: %v", err)
	}
	tx, txErr := f.pool.Begin(f.ctx)
	if txErr != nil {
		f.t.Fatalf("issue tx: %v", txErr)
	}
	seed := func(statement string, args ...any) {
		if _, err := tx.Exec(f.ctx, statement, args...); err != nil {
			_ = tx.Rollback(f.ctx)
			f.t.Fatalf("seed issue aggregate: %v", err)
		}
	}
	seed(`INSERT INTO repomesh_issues.creation_operations
		(project_id, actor, entry, creation_id, id, schema_version)
		VALUES ($1,$2,'issue_page',$3,$4,1)`,
		projectID, f.ownerID, "e2e-key-"+fmt.Sprintf("%d", time.Now().UnixNano()%1e9), operationID)
	seed(`INSERT INTO repomesh_issues.conversations
		(id, project_id, title, created_by_operation_id, title_origin_operation_id, content_scope_revision)
		VALUES ($1,$2,'e2e conversation',$3,$4,'e2escope')`,
		conversationID, projectID, operationID, operationID)
	seed(`INSERT INTO repomesh_issues.issues
		(id, project_id, number, title, description, criteria, revision, main_conversation_id, main_changeset_id,
		 initial_configuration_revision, creation_operation_id)
		VALUES ($1,$2, (SELECT next_number-1 FROM repomesh_issues.project_issue_counters WHERE project_id=$2),
			'e2e issue', 'e2e description', '[]'::jsonb, 'e2erev', $3, $4, $5, $6)`,
		issueID, projectID, conversationID, changeSetID, f.configRevision, operationID)
	seed(`INSERT INTO repomesh_issues.changesets (id, project_id, issue_id, kind)
		VALUES ($1,$2,$3,'main')`, changeSetID, projectID, issueID)
	if err := tx.Commit(f.ctx); err != nil {
		f.t.Fatalf("issue commit: %v", err)
	}
	return projectID, issueID
}

func newPool(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	return pgxpool.New(ctx, dsn)
}

// uuid4 derives deterministic UUID-v4-shaped strings from the fixture seed.
func (f *fixture) uuid4(salt byte) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s-%d", f.projectID, salt)))
	hexSum := hex.EncodeToString(sum[:])
	return fmt.Sprintf("%s-%s-4%s-8%s-%s",
		hexSum[0:8], hexSum[8:12], hexSum[13:16], hexSum[17:20], hexSum[20:32])
}
