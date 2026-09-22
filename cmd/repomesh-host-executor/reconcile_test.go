//go:build unix

package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"repomesh.local/repomesh/internal/execution"
	"repomesh.local/repomesh/internal/testdb"
)

// TestReconcileClosesLostRunAndReturnsTaskToDispatch 覆盖"任务一直执行不完"那条线上故障：
// host-executor 重启后等待丢失，run 永远停在 running，任务也就永远交不回经理门。
//
// 断言的重点不是"能收尾"，而是**收尾要如实**：exit_code 必须留空 —— 我们只知道进程没了，
// 不知道它退出码是多少，拿 -1 或 0 去填都是编数据。
func TestReconcileClosesLostRunAndReturnsTaskToDispatch(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()
	project := testdb.SeedProject(t, pool, "", "", "fixture/repo")
	testdb.SeedIssue(t, pool, project, "lost-issue", "fixture/repo")
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO repomesh_execution.workers(id, host, kind, active_attempts)
		VALUES('wrk_fixture','fixture-host','host_executor',1)`)

	var planID string
	if err := pool.QueryRow(ctx, `INSERT INTO public.plans(id, project_id, issue_id)
		VALUES(gen_random_uuid(), $1, 'lost-issue') RETURNING id::text`, project.ID).Scan(&planID); err != nil {
		t.Fatal(err)
	}
	const taskTitle = "Create health probe JSON config in app template"
	// organization_id 要用 fixture 的 **OrganizationID**（uuid）：tasks 上有
	// 「项目必须属于该组织」的复合外键，随手编一个 uuid 会被 23503 拒掉。
	var taskID string
	if err := pool.QueryRow(ctx, `INSERT INTO public.tasks(id, organization_id, project_id, repository_id, title, status, plan_id)
		VALUES(gen_random_uuid(), $1, $2, 'fixture/repo', $3, 'running', $4::uuid) RETURNING id::text`,
		project.OrganizationID, project.ID, taskTitle, planID).Scan(&taskID); err != nil {
		t.Fatal(err)
	}
	exec(`INSERT INTO public.plan_steps(id, plan_id, step_no, content, assignee_role, depends_on, status)
		VALUES(gen_random_uuid(), $1::uuid, 1, $2, 'worker', '[]', 'dispatched')`, planID, taskTitle)

	// 两条 attempt：一条真丢了（进程不在），一条还活着（必须原样不动）。
	const lostPid, livePid = int64(2073721), int64(4242)
	// 同一 issue 的 attempt 有 UNIQUE(project_id, issue_id, reservation_generation)，
	// 两条 fixture attempt 必须落在不同的 reservation 世代上。
	exec(`INSERT INTO repomesh_execution.attempts(id, project_id, issue_id, worker_id, configuration_revision, reservation_generation, state)
		VALUES('att_lost', $1, 'lost-issue', 'wrk_fixture', $2, 1, 'running')`, project.ID, project.ConfigurationID)
	exec(`INSERT INTO repomesh_execution.attempts(id, project_id, issue_id, worker_id, configuration_revision, reservation_generation, state)
		VALUES('att_live', $1, 'lost-issue', 'wrk_fixture', $2, 2, 'running')`, project.ID, project.ConfigurationID)
	exec(`INSERT INTO repomesh_execution.agent_runs(id, attempt_id, agent_kind, command, workspace, task_package_ref, state, pid, started_at)
		VALUES('run_lost','att_lost','codex_cli','codex exec','/tmp/fixture-workspace',$1,'running',$2, clock_timestamp())`, taskID, lostPid)
	exec(`INSERT INTO repomesh_execution.agent_runs(id, attempt_id, agent_kind, command, workspace, task_package_ref, state, pid, started_at)
		VALUES('run_live','att_live','codex_cli','codex exec','/tmp/fixture-workspace','plan:fixture','running',$1, clock_timestamp())`, livePid)

	service := execution.New(pool)
	closed, err := service.ReconcileLostRuns(ctx, "wrk_fixture", func(pid int64) bool { return pid == livePid })
	if err != nil {
		t.Fatal(err)
	}
	if closed != 1 {
		t.Fatalf("expected exactly one run closed, got %d", closed)
	}

	var state string
	var exitCode *int
	var exitedAt *string
	if err := pool.QueryRow(ctx, `SELECT state, exit_code, to_char(exited_at,'YYYY-MM-DD') FROM repomesh_execution.agent_runs WHERE id='run_lost'`).
		Scan(&state, &exitCode, &exitedAt); err != nil {
		t.Fatal(err)
	}
	if state != "lost" {
		t.Fatalf("lost run state = %q, want lost", state)
	}
	if exitCode != nil {
		t.Fatal("lost run must not invent an exit code: exit_code has to stay NULL")
	}
	if exitedAt == nil {
		t.Fatal("lost run must record when it was confirmed gone")
	}
	if err := pool.QueryRow(ctx, `SELECT state FROM repomesh_execution.agent_runs WHERE id='run_live'`).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "running" {
		t.Fatal("a run whose process is still alive must not be closed")
	}

	var attemptState string
	var stoppedAt *string
	if err := pool.QueryRow(ctx, `SELECT state, to_char(stopped_at,'YYYY-MM-DD') FROM repomesh_execution.attempts WHERE id='att_lost'`).
		Scan(&attemptState, &stoppedAt); err != nil {
		t.Fatal(err)
	}
	if attemptState != "stopped" || stoppedAt == nil {
		t.Fatalf("lost attempt state = %q stopped_at = %v, want stopped with a timestamp", attemptState, stoppedAt)
	}
	// 并发额度必须还回去，否则后续派发会一直 defer。
	var active int
	if err := pool.QueryRow(ctx, `SELECT active_attempts FROM repomesh_execution.workers WHERE id='wrk_fixture'`).Scan(&active); err != nil {
		t.Fatal(err)
	}
	if active != 0 {
		t.Fatalf("worker active_attempts = %d, want 0", active)
	}

	var taskStatus, summary, stepStatus string
	if err := pool.QueryRow(ctx, `SELECT status, result_summary FROM public.tasks WHERE id=$1::uuid`, taskID).
		Scan(&taskStatus, &summary); err != nil {
		t.Fatal(err)
	}
	if taskStatus != "pending" {
		t.Fatalf("task status = %q, want pending (retry within budget)", taskStatus)
	}
	if !contains(summary, "退出码未知") {
		t.Fatalf("task summary must state the unknown outcome honestly, got %q", summary)
	}
	if err := pool.QueryRow(ctx, `SELECT status FROM public.plan_steps WHERE plan_id=$1::uuid`, planID).Scan(&stepStatus); err != nil {
		t.Fatal(err)
	}
	if stepStatus != "ready" {
		t.Fatalf("plan step status = %q, want ready — otherwise the scheduler never picks it up again", stepStatus)
	}

	var events int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM repomesh_execution.attempt_events
		WHERE attempt_id='att_lost' AND kind='agent_lost'`).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if events != 1 {
		t.Fatalf("agent_lost evidence rows = %d, want 1", events)
	}
}

// seedProcessLossFixture 造一条任务，它名下已经有 priorLost 条"被部署撞丢"的开发 run
// （state='lost'），外加一条**刚丢的**（running、pid 已不存在）—— 后者才是这次对账要处理的。
//
// 为什么要有它（2026-09-22 用户："看看有没有卡点，有卡点就归档停止对应的任务"）：
// 此前重派额度数的是**全部**开发 run，于是 systemd 重启 executor（默认
// KillMode=control-group，会把跑在 executor cgroup 里的 agent 一起杀掉）造成的 lost
// 也照样吃掉额度。一条任务被部署撞三次就写成"自动重派已用满，需要人工介入"——
// 而它一次真正的尝试都没做过。台账里于是堆满假卡点，把真的埋掉。
func seedProcessLossFixture(t *testing.T, pool *pgxpool.Pool, priorLost int) (taskID, planID string) {
	t.Helper()
	ctx := context.Background()
	project := testdb.SeedProject(t, pool, "", "", "fixture/repo")
	testdb.SeedIssue(t, pool, project, "loss-issue", "fixture/repo")
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO repomesh_execution.workers(id, host, kind, active_attempts)
		VALUES('wrk_loss','fixture-host','host_executor',1)`)
	if err := pool.QueryRow(ctx, `INSERT INTO public.plans(id, project_id, issue_id)
		VALUES(gen_random_uuid(), $1, 'loss-issue') RETURNING id::text`, project.ID).Scan(&planID); err != nil {
		t.Fatal(err)
	}
	const taskTitle = "Loss budget fixture task"
	if err := pool.QueryRow(ctx, `INSERT INTO public.tasks(id, organization_id, project_id, repository_id, title, status, plan_id)
		VALUES(gen_random_uuid(), $1, $2, 'fixture/repo', $3, 'running', $4::uuid) RETURNING id::text`,
		project.OrganizationID, project.ID, taskTitle, planID).Scan(&taskID); err != nil {
		t.Fatal(err)
	}
	exec(`INSERT INTO public.plan_steps(id, plan_id, step_no, content, assignee_role, depends_on, status)
		VALUES(gen_random_uuid(), $1::uuid, 1, $2, 'worker', '[]', 'dispatched')`, planID, taskTitle)

	// priorLost 条已收尾的丢失 run：'lost' 要求 exited_at 非空、exit_code 留空（不编退出码）。
	for i := 0; i < priorLost; i++ {
		attempt := fmt.Sprintf("att_prior_%d", i)
		exec(`INSERT INTO repomesh_execution.attempts(id, project_id, issue_id, worker_id, configuration_revision, reservation_generation, state)
			VALUES($1, $2, 'loss-issue', 'wrk_loss', $3, $4, 'stopped')`, attempt, project.ID, project.ConfigurationID, i+1)
		exec(`INSERT INTO repomesh_execution.agent_runs(id, attempt_id, agent_kind, command, workspace, task_package_ref, state, started_at, exited_at)
			VALUES($1, $2, 'codex_cli', 'codex exec', '/tmp/fixture-workspace', $3, 'lost', clock_timestamp(), clock_timestamp())`,
			fmt.Sprintf("run_prior_%d", i), attempt, taskID)
	}
	// 刚丢的那一条：running，pid 已不存在 —— 这次对账的目标。
	exec(`INSERT INTO repomesh_execution.attempts(id, project_id, issue_id, worker_id, configuration_revision, reservation_generation, state)
		VALUES('att_now', $1, 'loss-issue', 'wrk_loss', $2, $3, 'running')`,
		project.ID, project.ConfigurationID, priorLost+1)
	exec(`INSERT INTO repomesh_execution.agent_runs(id, attempt_id, agent_kind, command, workspace, task_package_ref, state, pid, started_at)
		VALUES('run_now','att_now','codex_cli','codex exec','/tmp/fixture-workspace',$1,'running',2073721, clock_timestamp())`, taskID)
	return taskID, planID
}

// 部署重启把进程带走，**不该**算作"这条任务跑失败了"。
//
// 这里造 3 条已经丢过的 run（旧口径下正好把 3 次额度用满），再加一条刚丢的。
// 旧口径：任务 → failed「自动重派已用满，需要人工介入」—— 而它一次真正的尝试都没有。
// 新口径：任务 → pending（重新排队），且摘要里写明本次不计入额度。
func TestProcessLossDoesNotConsumeRetryBudget(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()
	taskID, planID := seedProcessLossFixture(t, pool, 3)

	service := execution.New(pool)
	closed, err := service.ReconcileLostRuns(ctx, "wrk_loss", func(int64) bool { return false })
	if err != nil {
		t.Fatal(err)
	}
	if closed != 1 {
		t.Fatalf("expected exactly one run closed, got %d", closed)
	}

	var taskStatus, summary string
	if err := pool.QueryRow(ctx, `SELECT status, result_summary FROM public.tasks WHERE id=$1::uuid`, taskID).
		Scan(&taskStatus, &summary); err != nil {
		t.Fatal(err)
	}
	if taskStatus != "pending" {
		t.Fatalf("task status = %q, want pending —— 进程丢失不是这条任务跑失败，不该吃掉重派额度（summary=%q）",
			taskStatus, summary)
	}
	if !contains(summary, "不计入") {
		t.Fatalf("摘要必须写明本次不计入重派额度，否则读的人以为额度又少了一次：%q", summary)
	}
	if !contains(summary, "退出码未知") {
		t.Fatalf("摘要仍要如实说明退出码未知（不编数据）：%q", summary)
	}
	var stepStatus string
	if err := pool.QueryRow(ctx, `SELECT status FROM public.plan_steps WHERE plan_id=$1::uuid`, planID).Scan(&stepStatus); err != nil {
		t.Fatal(err)
	}
	if stepStatus != "ready" {
		t.Fatalf("plan step = %q, want ready —— 否则调度器永远不会再挑它", stepStatus)
	}
}

// 额度豁免不能变成无限重派：环境一直丢进程（反复部署 / 反复 OOM）时必须有绝对上限。
// 到线就停在 failed 并说清"含进程丢失共 N 次"，交给人看。
func TestProcessLossStillStopsAtAbsoluteCap(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()
	taskID, _ := seedProcessLossFixture(t, pool, execution.MaxDevAttempts*4-1)

	service := execution.New(pool)
	if _, err := service.ReconcileLostRuns(ctx, "wrk_loss", func(int64) bool { return false }); err != nil {
		t.Fatal(err)
	}
	var taskStatus, summary string
	if err := pool.QueryRow(ctx, `SELECT status, result_summary FROM public.tasks WHERE id=$1::uuid`, taskID).
		Scan(&taskStatus, &summary); err != nil {
		t.Fatal(err)
	}
	if taskStatus != "failed" {
		t.Fatalf("task status = %q, want failed —— 到绝对上限就该停下，不能无限重派（summary=%q）",
			taskStatus, summary)
	}
	if !contains(summary, "含进程丢失") {
		t.Fatalf("到上限时要说清是含进程丢失一起算的：%q", summary)
	}
}

// TestProcessAliveSeesItselfAndNotAReapedChild 钉住探活判定的两个方向：
// 活着必须说活着（错杀会丢弃正在跑的产出），死了必须说死了（漏收就永远卡着）。
func TestProcessAliveSeesItselfAndNotAReapedChild(t *testing.T) {
	if !processAlive(int64(os.Getpid())) {
		t.Fatal("a live process was reported dead")
	}
	child := exec.Command("/bin/true")
	if err := child.Run(); err != nil {
		t.Fatal(err)
	}
	if processAlive(int64(child.Process.Pid)) {
		t.Fatal("a reaped child was reported alive")
	}
	if processAlive(0) {
		t.Fatal("pid 0 is not a live agent")
	}
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
