package main

import (
	"context"
	"strings"
	"testing"

	"repomesh.local/repomesh/internal/tasks"
	"repomesh.local/repomesh/internal/testdb"
)

// TestAutoApproveManagerGateOnlyForAutoMode 钉住两件事：
//  1. 「自动托管」的 issue 上，卡在经理门的任务会被 Leader 代行审批（用户报的
//     "我都用了自动模式了，应该让 leader 帮我审批的"）；
//  2. 「人工参与审计」的 issue 上**一行都不许动** —— 替人做主比不做事更坏。
//
// 另外钉住留痕：result_summary 必须写明是谁批的，以及单点验收到底有没有过。
func TestAutoApproveManagerGateOnlyForAutoMode(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()
	store := tasks.NewPostgresStore(pool)

	seed := func(issueID, mode, taskTitle string) (taskID string, planID string) {
		t.Helper()
		project := testdb.SeedProject(t, pool, "", "", "fixture/repo")
		testdb.SeedIssue(t, pool, project, issueID, "fixture/repo")
		if _, err := pool.Exec(ctx, `UPDATE repomesh_issues.issues SET hitl_mode=$2 WHERE id=$1`, issueID, mode); err != nil {
			t.Fatal(err)
		}
		if err := pool.QueryRow(ctx, `INSERT INTO public.plans(id, project_id, issue_id)
			VALUES(gen_random_uuid(), $1, $2) RETURNING id::text`, project.ID, issueID).Scan(&planID); err != nil {
			t.Fatal(err)
		}
		if err := pool.QueryRow(ctx, `INSERT INTO public.tasks(id, organization_id, project_id, repository_id, title, status, plan_id)
			VALUES(gen_random_uuid(), $1, $2, 'fixture/repo', $3, 'blocked', $4::uuid) RETURNING id::text`,
			project.OrganizationID, project.ID, taskTitle, planID).Scan(&taskID); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO public.plan_steps(id, plan_id, step_no, content, assignee_role, depends_on, status)
			VALUES(gen_random_uuid(), $1::uuid, 1, $2, 'worker', '[]', 'dispatched')`, planID, taskTitle); err != nil {
			t.Fatal(err)
		}
		return taskID, planID
	}

	autoTask, autoPlan := seed("gate-auto", "ai", "自动托管的任务")
	humanTask, humanPlan := seed("gate-hitl", "hitl", "人工参与的任务")
	// 自动模式那条带一条**通过**的单点验收，留痕里应当如实提到。
	if _, err := pool.Exec(ctx, `INSERT INTO public.test_evidence
		(id, project_id, issue_id, plan_id, task_id, repository_id, kind, script, command, exit_code, passed, summary, run_id, producer)
		SELECT gen_random_uuid(), project_id, $2, plan_id, id, 'fixture/repo', 'task_single_point',
		       't.test.ts', 'npm test', 0, true, 'fixture evidence', 'run_fixture', 'test_agent'
		FROM public.tasks WHERE id=$1::uuid`, autoTask, "gate-auto"); err != nil {
		t.Fatal(err)
	}

	approved, err := sweepAutoApproveManagerGate(ctx, pool, store, nil)
	if err != nil {
		t.Fatal(err)
	}
	if approved != 1 {
		t.Fatalf("approved = %d, want exactly 1（只该批自动托管那条）", approved)
	}

	var status, summary, stepStatus string
	if err := pool.QueryRow(ctx, `SELECT status, result_summary FROM public.tasks WHERE id=$1::uuid`, autoTask).
		Scan(&status, &summary); err != nil {
		t.Fatal(err)
	}
	if status != "done" {
		t.Fatalf("auto task status = %q, want done", status)
	}
	if !strings.Contains(summary, "自动托管") || !strings.Contains(summary, "单点验收已通过") {
		t.Fatalf("summary must record who approved and the evidence state, got %q", summary)
	}
	if err := pool.QueryRow(ctx, `SELECT status FROM public.plan_steps WHERE plan_id=$1::uuid`, autoPlan).Scan(&stepStatus); err != nil {
		t.Fatal(err)
	}
	if stepStatus != "done" {
		t.Fatalf("auto plan step = %q, want done", stepStatus)
	}

	if err := pool.QueryRow(ctx, `SELECT status FROM public.tasks WHERE id=$1::uuid`, humanTask).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "blocked" {
		t.Fatalf("hitl task status = %q, want blocked（人工模式不许代行）", status)
	}
	if err := pool.QueryRow(ctx, `SELECT status FROM public.plan_steps WHERE plan_id=$1::uuid`, humanPlan).Scan(&stepStatus); err != nil {
		t.Fatal(err)
	}
	if stepStatus != "dispatched" {
		t.Fatalf("hitl plan step = %q, want dispatched（不许被代行推进）", stepStatus)
	}
}
