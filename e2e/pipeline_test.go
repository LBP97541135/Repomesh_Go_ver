//go:build e2e

package e2e

// pipeline_test.go: the full-pipeline happy path plus refusal checks.

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"repomesh.local/repomesh/internal/assembly"
	"repomesh.local/repomesh/internal/branchvalidation"
	"repomesh.local/repomesh/internal/interfacedoc"
	"repomesh.local/repomesh/internal/observability"
	"repomesh.local/repomesh/internal/tasks"
)

// dbLedger implements tasks.ExecutionFacade against the live database: it
// registers a real worker row and a real attempt, so the dual dispatch writes
// satisfy the agent_runs FK and mirror what the host executor would do.
type dbLedger struct {
	pool      *pgxpool.Pool
	workerID  string
	issueID   string
	projectID string
}

func (l *dbLedger) ReserveForTask(ctx context.Context, workerID, taskID, agentKind, title, instruction string) (string, error) {
	if _, err := l.pool.Exec(ctx, `INSERT INTO repomesh_execution.workers (id, host, kind)
		VALUES ($1,'e2e-host','host_executor') ON CONFLICT (id) DO NOTHING`, workerID); err != nil {
		return "", err
	}
	attemptID := fmt.Sprintf("att_e2e_%d", time.Now().UnixNano()%1e9)
	if _, err := l.pool.Exec(ctx, `INSERT INTO repomesh_execution.attempts
		(id, project_id, issue_id, worker_id, configuration_revision, state, reservation_generation)
		VALUES ($1, $2, $4, $3, 'e2erev', 'reserved',
		 COALESCE((SELECT MAX(reservation_generation)+1 FROM repomesh_execution.attempts WHERE project_id=$2 AND issue_id=$4), 1))
		ON CONFLICT (id) DO NOTHING`, attemptID, l.projectID, workerID, l.issueID); err != nil {
		return "", err
	}
	return attemptID, nil
}

func TestFullPipeline(t *testing.T) {
	f := setup(t)
	projectID, issueID := f.seedProject()
	t.Logf("seeded project=%s issue=%s", projectID[:12], issueID[:12])
	store := tasks.NewPostgresStore(f.pool)

	// 1. org assembly: leader + manager + workers per repository (M4)
	assemblyService := assembly.New(f.pool, nil, nil)
	assembled, err := assemblyService.Assemble(f.ctx, assembly.AssemblyCommand{
		OrganizationID: f.orgUUID, Repositories: []string{"e2e/repo-a", "e2e/repo-b"},
		WorkersPerRepo: 2, LeaderName: "e2e-leader",
	})
	if err != nil {
		t.Fatalf("assembly: %v", err)
	}
	if assembled.LeaderAgentID == "" || len(assembled.Managers) != 2 {
		t.Fatalf("assembly incomplete: %+v", assembled)
	}
	t.Logf("PASS assembly: leader=%s managers=%d", assembled.LeaderAgentID[:12], len(assembled.Managers))

	// idempotent re-run: same singletons
	again, err := assemblyService.Assemble(f.ctx, assembly.AssemblyCommand{
		OrganizationID: f.orgUUID, Repositories: []string{"e2e/repo-a", "e2e/repo-b"},
		WorkersPerRepo: 2, LeaderName: "e2e-leader",
	})
	if err != nil || again.LeaderAgentID != assembled.LeaderAgentID {
		t.Fatalf("assembly not idempotent: %v %+v", err, again)
	}
	t.Log("PASS assembly idempotent")

	// 2. plan creation via the tasks axis
	plan, err := store.CreatePlan(f.ctx, tasks.PlanWrite{
		ProjectID:       projectID,
		RequirementText: "e2e requirement",
		RequirementKey:  fmt.Sprintf("e2e-key-%d", time.Now().UnixNano()%1e9),
		Batches:         [][]string{{"e2e/repo-a"}, {"e2e/repo-b"}},
		DAG:             map[string][]string{"e2e/repo-b": {"e2e/repo-a"}},
	})
	if err != nil {
		t.Fatalf("plan create: %v", err)
	}
	if plan.ID == "" {
		t.Fatal("plan id empty")
	}
	t.Logf("PASS plan created: %s", plan.ID)

	// 3. dependency promotion runs clean on a plan without deps
	if _, err := store.PromoteReady(f.ctx); err != nil {
		t.Fatalf("promote: %v", err)
	}
	t.Log("PASS promote runs clean")

	// 4. dual dispatch registers dev+test runs on one attempt (M5)
	// The dispatch path keys off a public.tasks row (the tasks axis); create
	// the task the plan step would have spawned, then dual-dispatch it.
	var taskRowID string
	if err := f.pool.QueryRow(f.ctx, `INSERT INTO public.tasks
		(id, organization_id, project_id, repository_id, title, instruction, acceptance)
		VALUES (gen_random_uuid(), $1::uuid, $1::uuid, 'e2e/repo-a', 'e2e task', 'do the thing', '')
		RETURNING id::text`, f.orgUUID).Scan(&taskRowID); err != nil {
		t.Fatalf("seed task row: %v", err)
	}
	dual, err := store.DispatchDual(f.ctx, &dbLedger{pool: f.pool, workerID: "wrk_e2e_runner", issueID: issueID, projectID: projectID}, "wrk_e2e_runner", taskRowID, "claude_cli", "e2e task", "do the thing")
	if err != nil {
		t.Fatalf("dual dispatch: %v", err)
	}
	if dual.DevelopmentAgentRunID == "" || dual.TestAgentRunID == "" {
		t.Fatalf("dual dispatch incomplete: %+v", dual)
	}
	var devCount, testCount int
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM repomesh_execution.agent_runs
		WHERE id=$1 AND agent_kind='claude_cli'`, dual.DevelopmentAgentRunID).Scan(&devCount); err != nil || devCount != 1 {
		t.Fatalf("dev run missing: count %d err %v", devCount, err)
	}
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM repomesh_execution.agent_runs
		WHERE id=$1 AND agent_kind='test_agent'`, dual.TestAgentRunID).Scan(&testCount); err != nil || testCount != 1 {
		t.Fatalf("test run missing: count %d err %v", testCount, err)
	}
	t.Logf("PASS dual dispatch: dev=%s test=%s attempt=%s",
		dual.DevelopmentAgentRunID[:12], dual.TestAgentRunID[:12], dual.AttemptID[:12])

	// 5. manager approve gate refuses unknown tasks
	if err := store.ApproveStep(f.ctx, "tsk_nonexistent", "mgr_e2e", "x"); err == nil {
		t.Fatal("approve of nonexistent task should fail")
	}
	t.Log("PASS approve refuses unknown task")

	// 6. interface document multi-approval (M7)
	docService := interfacedoc.New(f.pool)
	doc, err := docService.Create(f.ctx, interfacedoc.CreateCommand{
		ProjectID: projectID, IssueID: issueID, AuthorID: "mgr-a",
		Title: "e2e interface contract", Content: "POST /orders {id int}",
		Approvers: []string{"mgr-b", "mgr-c"},
	})
	if err != nil {
		t.Fatalf("doc create: %v", err)
	}
	if _, err := docService.Approve(f.ctx, doc.ID, "mgr-outsider"); err == nil {
		t.Fatal("outsider approval should be refused")
	}
	partial, err := docService.Approve(f.ctx, doc.ID, "mgr-b")
	if err != nil || partial.State != "awaiting_approval" {
		t.Fatalf("partial approval state=%s err=%v", partial.State, err)
	}
	final, err := docService.Approve(f.ctx, doc.ID, "mgr-c")
	if err != nil || final.State != "effective" || final.EffectiveAt == "" {
		t.Fatalf("final approval state=%s err=%v", final.State, err)
	}
	readBack, err := docService.Get(f.ctx, doc.ID)
	if err != nil || readBack.State != "effective" || len(readBack.ApprovedBy) != 2 {
		t.Fatalf("doc read-back: %+v err=%v", readBack, err)
	}
	t.Log("PASS interface doc: refuse outsider, partial, effective, read-back")

	// 7. branch validation lifecycle with the local provider (M8)
	sourceDB := fmt.Sprintf("e2e_tpl_%d", time.Now().UnixNano()%1000000)
	adminDSN := mustEnv("E2E_ADMIN_DSN", "postgres://repomesh:repomesh_pg_2026_x7k9@127.0.0.1:5432/postgres?sslmode=disable")
	if err := execStatement(f.ctx, adminDSN, fmt.Sprintf(`CREATE DATABASE %q`, sourceDB)); err != nil {
		t.Fatalf("template create: %v", err)
	}
	t.Cleanup(func() {
		_ = execStatement(context.Background(), adminDSN, fmt.Sprintf(`DROP DATABASE IF EXISTS %q`, sourceDB))
	})
	provider := &branchvalidation.LocalProvider{AdminDSN: adminDSN}
	bv := branchvalidation.New(f.pool, provider)
	run, err := bv.Start(f.ctx, branchvalidation.StartCommand{
		OrganizationID: f.orgUUID, ProjectID: projectID, RepositoryID: "e2e/repo-a",
		CandidateSHA: "e2ecafe123", SourceDatabaseRef: sourceDB,
		IdempotencyKey: fmt.Sprintf("e2e-bv-%d", time.Now().UnixNano()%1e9),
		Migrations:     []string{"CREATE TABLE e2e_probe(id int)", "CREATE TABLE e2e_probe2(id int)"},
	})
	if err != nil {
		t.Fatalf("branch validation start: %v", err)
	}
	if run.Status != "passed" {
		t.Fatalf("branch validation status=%s failure=%s results=%+v", run.Status, run.FailureCode, run.MigrationResults)
	}
	var branchLeft int
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM pg_database WHERE datname=$1`,
		run.BranchRef).Scan(&branchLeft); err != nil || branchLeft != 0 {
		t.Fatalf("branch not cleaned: %s (count %d err %v)", run.BranchRef, branchLeft, err)
	}
	second, err := bv.Start(f.ctx, branchvalidation.StartCommand{
		OrganizationID: f.orgUUID, ProjectID: projectID, RepositoryID: "e2e/repo-a",
		IdempotencyKey: fmt.Sprintf("e2e-bv-%d", time.Now().UnixNano()%1e9),
	})
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if second.ID == run.ID {
		t.Fatal("different keys must not replay the same run")
	}
	t.Logf("PASS branch validation: %s, branch cleaned, distinct runs (%s / %s)",
		run.Status, run.ID[:12], second.ID[:12])

	// 8. observability read surface (M9)
	obs := observability.New(f.pool)
	sessions, err := obs.ListTraceSessions(f.ctx, 10)
	if err != nil {
		t.Fatalf("trace sessions: %v", err)
	}
	logs, err := obs.ListLogs(f.ctx, "", 10)
	if err != nil {
		t.Fatalf("logs: %v", err)
	}
	t.Logf("PASS observability: %d sessions, %d log rows", len(sessions), len(logs))

	// 9. HTTP surface: honest health/refusal codes
	checkHTTP := func(path string, wantCode int) {
		t.Helper()
		resp, err := http.Get(f.base + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != wantCode {
			t.Fatalf("GET %s: got %d want %d", path, resp.StatusCode, wantCode)
		}
	}
	checkHTTP("/healthz", 200)
	checkHTTP("/readyz", 503)
	// Unauthenticated API access must be refused; the exact code depends on
	// deployment mode (401 with auth configured, 503 AUTH_NOT_CONFIGURED
	// without). Both are honest refusals; only a 200 would be a leak.
	resp, err := http.Get(f.base + "/api/projects")
	if err != nil {
		t.Fatalf("GET /api/projects: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != 401 && resp.StatusCode != 503 {
		t.Fatalf("GET /api/projects: got %d want 401/503", resp.StatusCode)
	}
	t.Logf("PASS http surface: healthz 200, readyz 503 honest, api refusal %d", resp.StatusCode)
}

func execStatement(ctx context.Context, dsn, statement string) error {
	pool, err := newPool(ctx, dsn)
	if err != nil {
		return err
	}
	defer pool.Close()
	_, err = pool.Exec(ctx, statement)
	return err
}
