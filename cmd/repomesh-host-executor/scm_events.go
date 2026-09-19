package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"

	"repomesh.local/repomesh/internal/execution"
)

// testEvidence 是测试 agent 写下的证据文件形状（task 单点与节点级集成共用）。
type testEvidence struct {
	Script   string `json:"script"`
	Command  string `json:"command"`
	ExitCode *int   `json:"exit_code"`
	Passed   *bool  `json:"passed"`
	Summary  string `json:"summary"`
}

// readTestEvidence 读回并解析证据文件。读不到/解不开/没有 passed 字段一律返回
// ok=false —— 宁可界面显示"还没有记录"，也不拿一个空的"通过"去骗合并闸门。
func readTestEvidence(workspace string) (testEvidence, bool) {
	// 2026-09-20 线上实测：测试 agent 的命令是 `cd repo` 之后再跑，所以它按提示词
	// 写下的 "test-evidence.json" 落在 **<workspace>/repo/** 下；而这里原先只看
	// 工作区根目录 —— 文件明明写好了却读不到，单点验收记录一直是 0 条。
	// 两处都找（集成 run 的脚本会把文件复制回根目录，单点 run 不会）。
	var raw []byte
	var err error
	for _, candidate := range []string{
		filepath.Join(workspace, execution.TestEvidenceFile),
		filepath.Join(workspace, "repo", execution.TestEvidenceFile),
	} {
		raw, err = os.ReadFile(candidate)
		if err == nil {
			break
		}
	}
	if err != nil {
		return testEvidence{}, false
	}
	var evidence testEvidence
	if json.Unmarshal(raw, &evidence) != nil || evidence.Passed == nil {
		return testEvidence{}, false
	}
	return evidence, true
}

// recordIntegrationEvidence 把节点级集成/联调 run 的证据落成一行 test_evidence。
//
// ref 的约定形状由 coordinator 的 integrationDispatcher.dispatch 写下：
// "plan:<planID>:kind:<kind>:repo:<repository>"。解析不出就什么都不写。
func (e *executor) recordIntegrationEvidence(ctx context.Context, runID, ref, workspace string, exitCode int) {
	parts := strings.Split(ref, ":")
	if len(parts) != 7 || parts[0] != "plan" || parts[2] != "kind" || parts[4] != "repo" {
		return
	}
	planID, kind, repository := parts[1], parts[3], parts[5]
	if kind != "repo_integration" && kind != "cross_repo_regression" {
		return
	}
	evidence, ok := readTestEvidence(workspace)
	if !ok {
		return
	}
	// 进程退出码非 0 时无论 agent 自述什么，都按不通过记 —— 事实优先于自述。
	passed := *evidence.Passed && exitCode == 0
	code := exitCode
	if evidence.ExitCode != nil {
		code = *evidence.ExitCode
	}
	var projectID, issueID string
	if err := e.pool.QueryRow(ctx, `SELECT project_id::text, COALESCE(issue_id,'')
		FROM public.plans WHERE id=$1::uuid`, planID).Scan(&projectID, &issueID); err != nil {
		return
	}
	if projectID == "" || issueID == "" {
		return
	}
	_, _ = e.pool.Exec(ctx, `INSERT INTO public.test_evidence
		(id, project_id, issue_id, plan_id, repository_id, kind,
		 script, command, exit_code, passed, summary, run_id, producer)
		VALUES (gen_random_uuid(), $1::uuid, $2, $3::uuid, $4, $5,
		 $6, $7, $8, $9, $10, $11, 'test_agent')`,
		projectID, issueID, planID, repository, kind,
		evidence.Script, evidence.Command, code, passed, evidence.Summary, runID)
}

// prURLLine 匹配交付脚本最后打的 REPO_PR_URL=https://github.com/o/r/pull/123。
var prURLLine = regexp.MustCompile(`REPO_PR_URL=(https://github\.com/\S+/pull/\d+)`)

// ensureChangeSet 取（没有就建）这条任务名下的 change set。
//
// 交付列车按任务列车厢，而 change_sets 这一行此前**没有任何生产者**（Freeze 没有
// 调用方）—— 所以列车永远是空车厢、"确认合并"也没有对象可合。
func (e *executor) ensureChangeSet(ctx context.Context, taskID string) string {
	var existing string
	err := e.pool.QueryRow(ctx, `SELECT id::text FROM public.change_sets
		WHERE task_id = $1::uuid ORDER BY version DESC LIMIT 1`, taskID).Scan(&existing)
	if err == nil {
		return existing
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return ""
	}
	var organizationID string
	if err := e.pool.QueryRow(ctx, `SELECT p.organization_id::text
		FROM public.tasks t JOIN repomesh_projects.projects p ON p.id = t.project_id::text
		WHERE t.id = $1::uuid`, taskID).Scan(&organizationID); err != nil {
		return ""
	}
	var created string
	if err := e.pool.QueryRow(ctx, `INSERT INTO public.change_sets (id, organization_id, task_id, status)
		VALUES (gen_random_uuid(), $1::uuid, $2::uuid, 'draft') RETURNING id::text`,
		organizationID, taskID).Scan(&created); err != nil {
		return ""
	}
	return created
}

// recordDeliveryFacts 把交付面**可观测的事实**记进 change set。
//
// 合并闸门（scm.Gate）要 pushed / PR / CI / reviewed 四项，而此前没有任何地方记录
// 它们 —— 闸门恒闭、交付段的"确认合并"永远回"闸门未开"。这里只记真的发生过的事：
//
//	· 交付 run 的 stdout 里有没有 REPO_PR_URL（有才记 push + pull_request，并落 pr_url）
//	· 测试 agent（双派工那条）的退出码（0 记 ci/recorded，非 0 记 ci/failed）
//
// 观测不到就什么都不记（闸门因此不开）—— 不补假数据。
func (e *executor) recordDeliveryFacts(ctx context.Context, runID, workspace string) {
	var agentKind, taskRef string
	var exitCode int
	if err := e.pool.QueryRow(ctx, `SELECT agent_kind, COALESCE(task_package_ref,''), COALESCE(exit_code,0)
		FROM repomesh_execution.agent_runs WHERE id=$1`, runID).Scan(&agentKind, &taskRef, &exitCode); err != nil {
		return
	}
	if taskRef == "" {
		return
	}
	// 集成 run 的引用是约定形状 "plan:<planID>:kind:<kind>:repo:<repository>"
	// （见 coordinator 的 integrationDispatcher.dispatch）：它不属于任何一条任务，
	// 所以走自己那条记账路，不能拿去 ensureChangeSet（那不是 uuid）。
	if strings.HasPrefix(taskRef, "plan:") {
		e.recordIntegrationEvidence(ctx, runID, taskRef, workspace, exitCode)
		return
	}
	changeSetID := e.ensureChangeSet(ctx, taskRef)
	if changeSetID == "" {
		return
	}
	if agentKind == "test_agent" {
		status, params := "recorded", `{"source":"test_agent","exitCode":0}`
		if exitCode != 0 {
			status, params = "failed", `{"source":"test_agent","exitCode":`+strconv.Itoa(exitCode)+`}`
		}
		_, _ = e.pool.Exec(ctx, `INSERT INTO public.scm_commands (id, change_set_id, command_type, params, status)
			VALUES (gen_random_uuid(), $1::uuid, 'ci', $2::jsonb, $3)`, changeSetID, params, status)
		// 2026-09-20：退出码之外还要留下**可查的单点验收记录**（脚本、命令、结论）。
		// 证据是测试 agent 自己写下的 test-evidence.json；读不到就不写行 ——
		// 界面因此显示"还没有记录"，而不是编一个通过。
		e.recordTestEvidence(ctx, runID, taskRef, workspace, exitCode)
		return
	}
	raw, err := os.ReadFile(filepath.Join(workspace, "agent-stdout.log"))
	if err != nil {
		return
	}
	match := prURLLine.FindStringSubmatch(string(raw))
	if match == nil {
		return
	}
	prURL := match[1]
	if _, err := e.pool.Exec(ctx, `UPDATE public.change_sets SET status='frozen', pr_url=$2 WHERE id=$1::uuid`,
		changeSetID, prURL); err != nil {
		return
	}
	for _, kind := range []string{"push", "pull_request"} {
		_, _ = e.pool.Exec(ctx, `INSERT INTO public.scm_commands (id, change_set_id, command_type, params, status)
			VALUES (gen_random_uuid(), $1::uuid, $2, $3::jsonb, 'recorded')`,
			changeSetID, kind, `{"pr":"`+prURL+`"}`)
	}
}

// recordTestEvidence 把测试 agent 写下的证据文件落成一行 test_evidence。
//
// 只记 agent 真的写下来的东西：脚本路径、它跑的命令、退出码、通过与否、一句话
// 结论。文件缺失或解不开就什么都不写（fail-closed：宁可界面显示"还没有记录"，
// 也不能拿一个空的"通过"去骗合并闸门）。
func (e *executor) recordTestEvidence(ctx context.Context, runID, taskRef, workspace string, exitCode int) {
	raw, err := os.ReadFile(filepath.Join(workspace, execution.TestEvidenceFile))
	if err != nil {
		return
	}
	var evidence struct {
		Script   string `json:"script"`
		Command  string `json:"command"`
		ExitCode *int   `json:"exit_code"`
		Passed   *bool  `json:"passed"`
		Summary  string `json:"summary"`
	}
	if json.Unmarshal(raw, &evidence) != nil || evidence.Passed == nil {
		return
	}
	// 进程退出码非 0 时，无论 agent 在文件里写了什么，都按不通过记 ——
	// 事实优先于自述。
	passed := *evidence.Passed && exitCode == 0
	code := exitCode
	if evidence.ExitCode != nil {
		code = *evidence.ExitCode
	}
	var projectID, issueID, planID, repositoryID string
	if err := e.pool.QueryRow(ctx, `SELECT t.project_id::text, COALESCE(scope.issue_id,''),
		       COALESCE(t.plan_id::text,''), COALESCE(scope.repository_id,'')
		FROM public.tasks t
		LEFT JOIN public.task_repository_scopes scope ON scope.task_id = t.id
		WHERE t.id=$1::uuid`, taskRef).Scan(&projectID, &issueID, &planID, &repositoryID); err != nil {
		return
	}
	if projectID == "" || issueID == "" {
		return
	}
	var plan any
	if planID != "" {
		plan = planID
	}
	_, _ = e.pool.Exec(ctx, `INSERT INTO public.test_evidence
		(id, project_id, issue_id, plan_id, task_id, repository_id, kind,
		 script, command, exit_code, passed, summary, run_id, producer)
		VALUES (gen_random_uuid(), $1::uuid, $2, $3::uuid, $4::uuid, $5, 'task_single_point',
		 $6, $7, $8, $9, $10, $11, 'test_agent')`,
		projectID, issueID, plan, taskRef, repositoryID,
		evidence.Script, evidence.Command, code, passed, evidence.Summary, runID)
}
