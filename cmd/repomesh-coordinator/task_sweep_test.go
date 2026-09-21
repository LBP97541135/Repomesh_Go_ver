package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"repomesh.local/repomesh/internal/testdb"
)

// TestSweepClosesTaskWithNoLiveRun 覆盖"任务标着在跑、却没有任何在跑的 run"那条线上故障
// （repomesh-e2e-api 的"验证满700免运费功能"从 9-20 挂在 running，四个 run 全都已终结）。
//
// 同时钉住**不能误伤**的两种情形：还有 run 在跑的、以及刚结束还在冷却期内的。
// 误伤会把正在跑的任务判死并重派，代价比漏收大得多。
func TestSweepClosesTaskWithNoLiveRun(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()
	project := testdb.SeedProject(t, pool, "", "", "fixture/repo")
	testdb.SeedIssue(t, pool, project, "sweep-issue", "fixture/repo")

	insertTask := func(title, status string) (taskID, planID string) {
		t.Helper()
		if err := pool.QueryRow(ctx, `INSERT INTO public.plans(id, project_id, issue_id)
			VALUES(gen_random_uuid(), $1, 'sweep-issue') RETURNING id::text`, project.ID).Scan(&planID); err != nil {
			t.Fatal(err)
		}
		if err := pool.QueryRow(ctx, `INSERT INTO public.tasks(id, organization_id, project_id, repository_id, title, status, plan_id)
			VALUES(gen_random_uuid(), $1, $2, 'fixture/repo', $3, $4, $5::uuid) RETURNING id::text`,
			project.OrganizationID, project.ID, title, status, planID).Scan(&taskID); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO public.plan_steps(id, plan_id, step_no, content, assignee_role, depends_on, status)
			VALUES(gen_random_uuid(), $1::uuid, 1, $2, 'worker', '[]', 'dispatched')`, planID, title); err != nil {
			t.Fatal(err)
		}
		return taskID, planID
	}
	// 每行 fixture 都要一条 attempt（agent_runs 的外键指向它）。
	insertAttempt := func(attemptID string, generation int) {
		t.Helper()
		if _, err := pool.Exec(ctx, `INSERT INTO repomesh_execution.workers(id, host, kind)
			VALUES('wrk_sweep','fixture-host','host_executor') ON CONFLICT (id) DO NOTHING`); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO repomesh_execution.attempts(id, project_id, issue_id, worker_id, configuration_revision, reservation_generation, state)
			VALUES($1, $2, 'sweep-issue', 'wrk_sweep', $3, $4, 'stopped')`,
			attemptID, project.ID, project.ConfigurationID, generation); err != nil {
			t.Fatal(err)
		}
	}
	insertRun := func(runID, attemptID, taskID, state string, exitedAgo time.Duration) {
		t.Helper()
		if _, err := pool.Exec(ctx, `INSERT INTO repomesh_execution.agent_runs
			(id, attempt_id, agent_kind, command, workspace, task_package_ref, state, pid, started_at, exit_code, exited_at)
			VALUES($1, $2, 'codex_cli', 'codex exec', '/tmp/fixture', $3, $4,
			        CASE WHEN $4='running' THEN 4242 ELSE NULL END,
			        clock_timestamp() - interval '20 minutes',
			        CASE WHEN $4 IN ('exited','killed') THEN 1 ELSE NULL END,
			        CASE WHEN $4 IN ('exited','killed') THEN clock_timestamp() - $5::interval ELSE NULL END)`,
			runID, attemptID, taskID, state, exitedAgo.String()); err != nil {
			t.Fatal(err)
		}
	}

	staleTask, stalePlan := insertTask("验证满700免运费功能", "running")
	insertAttempt("att_sweep_stale", 1)
	insertRun("run_sweep_stale", "att_sweep_stale", staleTask, "exited", 10*time.Minute)

	liveTask, _ := insertTask("还有 run 在跑的任务", "running")
	insertAttempt("att_sweep_live", 2)
	insertRun("run_sweep_live", "att_sweep_live", liveTask, "running", 0)

	freshTask, _ := insertTask("刚刚结束、还在冷却期", "running")
	insertAttempt("att_sweep_fresh", 3)
	insertRun("run_sweep_fresh", "att_sweep_fresh", freshTask, "exited", 30*time.Second)

	swept, err := sweepStaleRunningTasks(ctx, pool, 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if swept != 1 {
		t.Fatalf("swept = %d, want exactly 1", swept)
	}

	var status, summary string
	if err := pool.QueryRow(ctx, `SELECT status, result_summary FROM public.tasks WHERE id=$1::uuid`, staleTask).
		Scan(&status, &summary); err != nil {
		t.Fatal(err)
	}
	if status != "pending" {
		t.Fatalf("stale task status = %q, want pending (retry within budget)", status)
	}
	if !strings.Contains(summary, "没有任何在跑的 run") {
		t.Fatalf("stale task summary must state the fact, got %q", summary)
	}
	var stepStatus string
	if err := pool.QueryRow(ctx, `SELECT status FROM public.plan_steps WHERE plan_id=$1::uuid`, stalePlan).Scan(&stepStatus); err != nil {
		t.Fatal(err)
	}
	if stepStatus != "ready" {
		t.Fatalf("stale plan step status = %q, want ready", stepStatus)
	}
	for name, id := range map[string]string{"还有 run 在跑": liveTask, "冷却期内": freshTask} {
		var got string
		if err := pool.QueryRow(ctx, `SELECT status FROM public.tasks WHERE id=$1::uuid`, id).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != "running" {
			t.Fatalf("%s 的任务被误伤：status = %q, want running", name, got)
		}
	}
}
