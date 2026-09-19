package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strconv"

	"github.com/jackc/pgx/v5"
)

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
