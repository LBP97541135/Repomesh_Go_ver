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
		if d.dispatch(ctx, planID, issueID, projectID, repo, "repo_integration", repos) {
			dispatched = true
		} else {
			d.recordExhausted(ctx, planID, issueID, projectID, repo, "repo_integration")
		}
	}
	// 跨仓库联调只在真的跨仓库时有意义：单仓库计划派它等于自己跟自己联调。
	// 每个仓库各派一条（每条验自己那一侧），聚合起来才是这次跨仓库回归。
	if len(repos) > 1 {
		for _, repo := range repos {
			if d.dispatch(ctx, planID, issueID, projectID, repo, "cross_repo_regression", repos) {
				dispatched = true
			} else {
				d.recordExhausted(ctx, planID, issueID, projectID, repo, "cross_repo_regression")
			}
		}
	} else {
		// 2026-09-20：单仓库计划此前**什么都不记** —— 于是"跨仓库联调"这一项在记录里
		// 既不是通过也不是跳过，而是彻底不存在，读的人分不清"没做"和"不需要做"。
		// 这里如实记一条：这次计划没有跨仓库依赖，联调不适用。
		d.recordSkipped(ctx, planID, issueID, projectID, repos[0],
			"本次计划只涉及一个仓库，没有跨仓库依赖 —— 跨仓库联调与回归不适用（不是失败，也不是漏做）。")
	}
	return dispatched
}

// recordSkipped 如实记一条"不适用"的证据，让读的人能区分「没做」与「不需要做」。
func (d *integrationDispatcher) recordSkipped(ctx context.Context, planID, issueID, projectID, repository, reason string) {
	var existing int
	if err := d.pool.QueryRow(ctx, `SELECT count(*) FROM public.test_evidence
		WHERE plan_id=$1::uuid AND kind='cross_repo_regression'`, planID).Scan(&existing); err != nil || existing > 0 {
		return
	}
	_, _ = d.pool.Exec(ctx, `INSERT INTO public.test_evidence
		(id, project_id, issue_id, plan_id, repository_id, kind,
		 script, command, exit_code, passed, summary, run_id, producer)
		VALUES (gen_random_uuid(), $1::uuid, $2, $3::uuid, $4, 'cross_repo_regression',
		 '', '', NULL, true, $5, '', 'coordinator')`,
		projectID, issueID, planID, repository, reason)
}

// recordExhausted 在"重派额度用满、却仍然没有证据"时**如实记一行不通过**。
//
// 不这么做的话，这条计划会永远停在"没有集成记录"的状态：界面上什么都不显示，
// 而实际上集成验证已经失败过两次了。记下这一行之后 tick 的准入条件不再成立，
// 循环也就此打住 —— 停下来并说清，而不是安静地转下去。
func (d *integrationDispatcher) recordExhausted(ctx context.Context, planID, issueID, projectID, repository, kind string) {
	ref := fmt.Sprintf("plan:%s:kind:%s:repo:%s", planID, kind, repository)
	var inFlight, total int
	if err := d.pool.QueryRow(ctx, `SELECT
		   count(*) FILTER (WHERE state IN ('pending','running')), count(*)
		FROM repomesh_execution.agent_runs WHERE task_package_ref=$1`, ref).Scan(&inFlight, &total); err != nil {
		return
	}
	if inFlight > 0 || total < 2 {
		return
	}
	var existing int
	if err := d.pool.QueryRow(ctx, `SELECT count(*) FROM public.test_evidence
		WHERE plan_id=$1::uuid AND kind=$2 AND repository_id=$3`, planID, kind, repository).Scan(&existing); err != nil || existing > 0 {
		return
	}
	_, _ = d.pool.Exec(ctx, `INSERT INTO public.test_evidence
		(id, project_id, issue_id, plan_id, repository_id, kind,
		 script, command, exit_code, passed, summary, run_id, producer)
		VALUES (gen_random_uuid(), $1::uuid, $2, $3::uuid, $4, $5,
		 '', '', NULL, false, $6, '', 'coordinator')`,
		projectID, issueID, planID, repository, kind,
		fmt.Sprintf("集成验证已自动重派 %d 次仍未产出证据（agent 没有写下 %s），需要人工介入", total, execution.TestEvidenceFile))
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
func (d *integrationDispatcher) dispatch(ctx context.Context, planID, issueID, projectID, repository, kind string, allRepos []string) bool {
	// 2026-09-20 线上实测（我自己的第一版就踩了）：tick 的准入条件是"这条计划还
	// 没有集成证据"，而证据要等 run **跑完**才写 —— run 还在飞的那段时间里条件
	// 一直成立，于是每 500ms 重派一条，几秒钟堆出几十条待跑 run。这里按**精确引用**
	// 兜底：同一个 (计划, 种类, 仓库) 在飞时不再派，且总数封顶 2 次（一次失败重试），
	// 免得证据写不出来时无限重派。
	ref := fmt.Sprintf("plan:%s:kind:%s:repo:%s", planID, kind, repository)
	var inFlight, total int
	if err := d.pool.QueryRow(ctx, `SELECT
		   count(*) FILTER (WHERE state IN ('pending','running')),
		   count(*)
		FROM repomesh_execution.agent_runs WHERE task_package_ref=$1`, ref).Scan(&inFlight, &total); err != nil {
		return false
	}
	if inFlight > 0 || total >= 2 {
		return false
	}
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
	prompt := integrationPrompt(kind, repository, allRepos)
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
	if _, err := tx.Exec(ctx, `INSERT INTO repomesh_execution.agent_runs
		(id, attempt_id, agent_kind, command, workspace, task_package_ref, state, repo_full_name)
		VALUES ($1,$2,'test_agent',$3,$4,$5,'pending',$6)`,
		runID, attemptID, buildIntegrationCommand(agentKind, model, repository), workspace, ref, repository); err != nil {
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
func integrationPrompt(kind, repository string, allRepos []string) string {
	scope := "本仓库（" + repository + "）"
	work := "把该仓库在集成态下跑起来：装依赖、跑它既有的测试与构建，确认改动合在一起仍然成立。"
	if kind == "cross_repo_regression" {
		scope = "本次计划涉及的全部仓库：" + strings.Join(allRepos, "、") + "（你手上检出的是 " + repository + "）"
		work = "做跨仓库联调与回归：结合上面列出的其它仓库，确认本次改动在 " + repository +
			" 这一侧仍然成立、仓库之间的接口约定没有被破坏，并跑一遍该仓库既有的测试。" +
			"你只能看到自己检出的那个仓库 —— 对看不到的仓库不要臆测，直接说清哪些结论无法在本仓库内验证。"
	}
	return "你是 RepoMesh 的测试 agent，负责**集成验证**，不要修改任何业务代码。\n\n" +
		"范围：" + scope + "。\n\n" +
		work + "\n\n" +
		"然后把结论写进当前目录下的 " + execution.TestEvidenceFile + "，只写这个 JSON：\n" +
		"{\"script\":\"<你写的验证脚本路径，没写脚本就填空串>\",\"command\":\"<你实际跑的命令>\"," +
		"\"exit_code\":<整数>,\"passed\":<true|false>,\"summary\":\"<一行，你实际观察到了什么>\"}\n\n" +
		"验证不过就 passed=false 并说清哪里不过 —— 不要编结果，也不要为了通过而放宽检查。\n"
}

// buildIntegrationCommand 与交付脚本同一约定：先铸令牌克隆**目标仓库**，再在工作区
// 里跑 agent；prompt 走工作区文件（不拼进 argv，否则 sanitize 会把引号抹掉）。
//
// 2026-09-20：第一版没克隆、也没给 repo_full_name，集成 agent 的工作区是空的 ——
// 它手上没有仓库，所谓"集成验证"只能靠猜。这里补上克隆，executor 据 repo_full_name
// 现场铸该仓库的 installation token（同交付 run）。
func buildIntegrationCommand(agentKind, model, repository string) string {
	agent := "codex exec -c model_provider=minimax -c model=" + model +
		" --skip-git-repo-check --dangerously-bypass-approvals-and-sandbox \"$(cat ../prompt.txt)\""
	if agentKind == "claude_cli" {
		agent = "claude -p \"$(cat ../prompt.txt)\" --model " + model + " --dangerously-skip-permissions"
	}
	script := "set -e\n" +
		"T=${REPOMESH_GH_TOKEN:?missing installation token}\n" +
		"git clone --depth 5 https://x-access-token:$T@github.com/" + repository + ".git repo\n" +
		"cd repo\n" +
		agent + "\n" +
		"cp " + execution.TestEvidenceFile + " ../" + execution.TestEvidenceFile + " 2>/dev/null || true"
	return "bash -c '" + script + "'"
}
