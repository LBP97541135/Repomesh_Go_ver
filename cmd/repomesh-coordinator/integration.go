package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"repomesh.local/repomesh/internal/execution"
)

// integrationDispatcher 负责**节点级**的测试：一个 DAG 节点（一个仓库）的所有任务
// 都过了经理门之后，还要做一次本仓库集成；计划跨多个仓库时，再做一次跨仓库联调 + 回归。
//
// 2026-09-20 线上实测：此前平台只有**任务级**的双派工（开发 run + 单点测试 run），
// DAG 节点级的集成与联调**一个字节都没有** —— 一个仓库改完就直接进交付列车，没有人
// 回答"这个仓库自己还能不能跑起来""上游改了之后下游还成立吗"。
//
// 复用规划派发那一套：挂活跃 host_executor worker 的 attempt、prompt 写工作区文件
// （不拼进 argv，避免 sanitize 把引号抹掉）、产物由 agent 写成 test-evidence.json，
// executor 读回入库。
type integrationDispatcher struct {
	pool *pgxpool.Pool
	root string
}

func newIntegrationDispatcher(pool *pgxpool.Pool) *integrationDispatcher {
	return &integrationDispatcher{pool: pool, root: "/opt/repomesh/workspaces"}
}

// tick 找**一条**可以开始集成的计划，给它派一轮集成 run。返回是否做了事。
func (d *integrationDispatcher) tick(ctx context.Context) bool {
	var planID, issueID, projectID string
	err := d.pool.QueryRow(ctx, `
		SELECT p.id::text, COALESCE(p.issue_id,''), p.project_id::text
		FROM public.plans p
		WHERE COALESCE(p.issue_id,'') <> ''
		  AND EXISTS (SELECT 1 FROM public.tasks t WHERE t.plan_id = p.id)
		  AND NOT EXISTS (SELECT 1 FROM public.tasks t WHERE t.plan_id = p.id AND t.status <> 'done')
		  AND NOT EXISTS (SELECT 1 FROM public.test_evidence e
		                  WHERE e.plan_id = p.id AND e.kind IN ('repo_integration','cross_repo_regression'))
		ORDER BY p.id LIMIT 1`).Scan(&planID, &issueID, &projectID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false
	}
	if err != nil {
		return false
	}
	repos, err := d.planRepositories(ctx, planID)
	if err != nil || len(repos) == 0 {
		return false
	}
	dispatched := false
	for _, repo := range repos {
		if d.dispatch(ctx, planID, issueID, projectID, repo, "repo_integration") {
			dispatched = true
		}
	}
	// 跨仓库联调只在真的跨仓库时有意义：单仓库计划派它等于自己跟自己联调。
	if len(repos) > 1 {
		if d.dispatch(ctx, planID, issueID, projectID, "", "cross_repo_regression") {
			dispatched = true
		}
	}
	return dispatched
}

func (d *integrationDispatcher) planRepositories(ctx context.Context, planID string) ([]string, error) {
	rows, err := d.pool.Query(ctx, `SELECT DISTINCT repository_id FROM public.tasks
		WHERE plan_id=$1::uuid AND COALESCE(repository_id,'') <> '' ORDER BY 1`, planID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var repo string
		if err := rows.Scan(&repo); err != nil {
			return nil, err
		}
		out = append(out, repo)
	}
	return out, rows.Err()
}

// dispatch 派一条集成 run。task_package_ref 用约定形状
// "plan:<planID>:kind:<kind>:repo:<repository>"，executor 据此把证据记到对的位置
// （集成 run 不属于任何一条任务，所以不能塞 task id）。
func (d *integrationDispatcher) dispatch(ctx context.Context, planID, issueID, projectID, repository, kind string) bool {
	var workerID string
	err := d.pool.QueryRow(ctx, `SELECT id FROM repomesh_execution.workers
		WHERE kind='host_executor' AND retired_at IS NULL
		  AND heartbeat_at > now() - interval '5 minutes'
		ORDER BY heartbeat_at DESC LIMIT 1`).Scan(&workerID)
	if err != nil || workerID == "" {
		return false
	}
	var revision string
	if err := d.pool.QueryRow(ctx, `SELECT initial_configuration_revision
		FROM repomesh_issues.issues WHERE id=$1`, issueID).Scan(&revision); err != nil {
		return false
	}
	var agentKind, model string
	_ = d.pool.QueryRow(ctx, `SELECT COALESCE(agent_kind,''), COALESCE(model,'')
		FROM repomesh_projects.agent_settings WHERE project_id=$1`, projectID).Scan(&agentKind, &model)
	if agentKind != "codex_cli" && agentKind != "claude_cli" {
		agentKind = "codex_cli"
	}
	if strings.TrimSpace(model) == "" {
		model = "MiniMax-M2"
	}
	attemptID, err := newRunID("att_int_")
	if err != nil {
		return false
	}
	workspace := filepath.Join(d.root, attemptID)
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		return false
	}
	prompt := integrationPrompt(kind, repository)
	if err := os.WriteFile(filepath.Join(workspace, "prompt.txt"), []byte(prompt), 0o644); err != nil {
		return false
	}
	runID, err := newRunID("run_int_")
	if err != nil {
		return false
	}
	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return false
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `INSERT INTO repomesh_execution.attempts
		(id, project_id, issue_id, worker_id, configuration_revision, state, reservation_generation, launch_verified_at)
		VALUES ($1,$2,$3,$4,$5,'launch_verified',
		 COALESCE((SELECT MAX(reservation_generation)+1 FROM repomesh_execution.attempts WHERE project_id=$2 AND issue_id=$3), 1),
		 clock_timestamp())
		ON CONFLICT (project_id, issue_id, reservation_generation) DO NOTHING`,
		attemptID, projectID, issueID, workerID, revision); err != nil {
		return false
	}
	ref := fmt.Sprintf("plan:%s:kind:%s:repo:%s", planID, kind, repository)
	if _, err := tx.Exec(ctx, `INSERT INTO repomesh_execution.agent_runs
		(id, attempt_id, agent_kind, command, workspace, task_package_ref, state)
		VALUES ($1,$2,'test_agent',$3,$4,$5,'pending')`,
		runID, attemptID, buildIntegrationCommand(agentKind, model), workspace, ref); err != nil {
		return false
	}
	if err := tx.Commit(ctx); err != nil {
		return false
	}
	fmt.Fprintf(os.Stderr, "coordinator: integration dispatched plan=%s kind=%s repo=%s run=%s\n",
		planID, kind, repository, runID)
	return true
}

// integrationPrompt 让集成 agent 只做**验证**：它不改代码，只回答"这个范围还能不能
// 跑"，并把结论写成机器可读的证据文件。
func integrationPrompt(kind, repository string) string {
	scope := "本仓库（" + repository + "）"
	work := "把该仓库在集成态下跑起来：装依赖、跑它既有的测试与构建，确认改动合在一起仍然成立。"
	if kind == "cross_repo_regression" {
		scope = "本次计划涉及的全部仓库"
		work = "做跨仓库联调与回归：确认上游仓库的改动在下游仍然成立、仓库之间的接口约定没有被破坏，并跑一遍各仓库既有的测试。"
	}
	return "你是 RepoMesh 的测试 agent，负责**集成验证**，不要修改任何业务代码。\n\n" +
		"范围：" + scope + "。\n\n" +
		work + "\n\n" +
		"然后把结论写进当前目录下的 " + execution.TestEvidenceFile + "，只写这个 JSON：\n" +
		"{\"script\":\"<你写的验证脚本路径，没写脚本就填空串>\",\"command\":\"<你实际跑的命令>\"," +
		"\"exit_code\":<整数>,\"passed\":<true|false>,\"summary\":\"<一行，你实际观察到了什么>\"}\n\n" +
		"验证不过就 passed=false 并说清哪里不过 —— 不要编结果，也不要为了通过而放宽检查。\n"
}

// buildIntegrationCommand 与规划派发同一约定：prompt 走工作区文件，不拼进 argv。
func buildIntegrationCommand(agentKind, model string) string {
	if agentKind == "claude_cli" {
		return "bash -c 'claude -p \"$(cat prompt.txt)\" --model " + model + " --dangerously-skip-permissions'"
	}
	return "bash -c 'codex exec -c model_provider=minimax -c model=" + model +
		" --skip-git-repo-check --dangerously-bypass-approvals-and-sandbox \"$(cat prompt.txt)\"'"
}