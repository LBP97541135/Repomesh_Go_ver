package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// coordinatorLedger adapts the coordinator to the tasks.ExecutionFacade: it
// reserves real worker attempts in the B10 ledger so the DAG dispatch path
// runs identically to the host-executor path (same rows, same invariants).
type coordinatorLedger struct {
	pool *pgxpool.Pool
}

// newRunID returns a random run/attempt id; the old timestamp-derived ids
// collided under concurrent dispatches and the ON CONFLICT DO NOTHING
// swallowed the insert, leaving the caller with a dangling attempt id.
func newRunID(prefix string) (string, error) {
	buffer := make([]byte, 10)
	if _, err := rand.Read(buffer); err != nil {
		return "", fmt.Errorf("coordinator: id generation failed: %w", err)
	}
	return prefix + hex.EncodeToString(buffer), nil
}

// sanitizeSingleQuoted makes text safe inside the single-quoted argv token
// restored by the executor's quote-aware splitter (and inside double-quoted
// git/curl arguments): quotes, NUL and shell-active characters are dropped.
func sanitizeSingleQuoted(text string) string {
	return strings.Map(func(r rune) rune {
		if r == '\'' || r == '"' || r == '`' || r == '$' || r == 0 {
			return ' '
		}
		return r
	}, text)
}

// buildAgentCommand assembles the full delivery sequence the host executor
// launches: clone as the GitHub App (repomesh-bot), run the coding agent on
// the real requirement, commit, push a delivery branch and open the pull
// request. The App installation token is read from the cache file refreshed
// by repomesh-gh-token.timer — never embedded into the stored command.
func buildAgentCommand(agentKind, model, instruction, repoFullName, attemptID, issueTitle, issueID string) (string, error) {
	prompt := strings.TrimSpace(instruction)
	if prompt == "" {
		prompt = "Complete the assigned task in this repository. Implement the requirement, run the existing checks, commit your changes with a summary."
	}
	prompt = sanitizeSingleQuoted(prompt)
	safeTitle := sanitizeSingleQuoted(issueTitle)
	if len([]rune(safeTitle)) > 60 {
		safeTitle = string([]rune(safeTitle)[:60])
	}
	var agentLine string
	switch agentKind {
	case "codex_cli":
		// Prompt travels in DOUBLE quotes: the whole script is wrapped in one
		// outer single-quote pair, and any inner single quote would close it
		// early — silently truncating the delivery sequence after the agent.
		agentLine = fmt.Sprintf("codex exec -c model_provider=minimax -c model=%s --skip-git-repo-check --dangerously-bypass-approvals-and-sandbox \"%s\"", model, prompt)
	case "claude_cli":
		agentLine = fmt.Sprintf("claude -p \"%s\" --model %s --dangerously-skip-permissions", prompt, model)
	default:
		return "", fmt.Errorf("coordinator: unsupported agent kind %q", agentKind)
	}
	// The whole sequence travels as ONE argv token (bash -c '...'); the
	// executor's quote-aware splitter restores it intact. Inside the script
	// only double quotes appear, so no shell quoting escapes the single pair.
	script := "set -e\n" +
		"T=$(cat /opt/repomesh/workspaces/.gh-token)\n" +
		"R=" + repoFullName + "\n" +
		"B=repomesh/auto-" + attemptID + "\n" +
		"git clone --depth 5 https://x-access-token:$T@github.com/$R.git repo\n" +
		"cd repo\n" +
		"git config user.name \"repomesh-bot[bot]\"\n" +
		"git config user.email \"repomesh-bot@users.noreply.github.com\"\n" +
		agentLine + "\n" +
		"git add -A\n" +
		"git diff --cached --quiet || git commit -m \"RepoMesh delivery " + issueID + ": " + safeTitle + "\"\n" +
		"git push origin HEAD:$B\n" +
		"curl -sf -X POST -H \"Authorization: Bearer $T\" -H \"Accept: application/vnd.github+json\" " +
		"https://api.github.com/repos/$R/pulls " +
		"-d \"{\\\"title\\\":\\\"RepoMesh: " + safeTitle + "\\\",\\\"head\\\":\\\"$B\\\",\\\"base\\\":\\\"main\\\",\\\"body\\\":\\\"RepoMesh automated delivery for issue " + issueID + "\\\"}\"\n" +
		"echo REPO_PR_CREATED=$R:$B"
	return "bash -c '" + script + "'", nil
}

// buildTestCommand assembles the test-agent sequence. It runs in the SAME
// attempt workspace the development agent just used (so the delivered change
// is right there under repo/), inspects that change, writes a test script for
// it and runs it — the single-point acceptance the delivery chain was missing.
//
// Before this, every plan step was assignee_role='worker' and the only
// "verification" was a human reading the pull request: agent_runs carried no
// test evidence at all (contract's test_command/test_results stayed null/[]).
// The exit code of this run is the platform's first machine-checkable signal.
func buildTestCommand(agentKind, model, instruction, repoFullName, attemptID, issueTitle string) (string, error) {
	requirement := strings.TrimSpace(instruction)
	if requirement == "" {
		requirement = strings.TrimSpace(issueTitle)
	}
	if requirement == "" {
		requirement = "the delivered change"
	}
	// 单引号会让外层 bash -c '...' 提前闭合（git 段静默丢失、exit 0 假成功），
	// 与 buildAgentCommand 同一道 sanitize。
	requirement = sanitizeSingleQuoted(requirement)
	testPrompt := "You are the test agent for this repository. " +
		"Requirement: " + requirement + ". " +
		"Inspect the change the development agent just delivered (git diff origin/main...HEAD). " +
		"Write a test script that verifies the requirement, run it, and report the exact command you ran, " +
		"its exit code and a one-line summary. If the change does not satisfy the requirement, " +
		"say so explicitly instead of reporting success."
	testPrompt = sanitizeSingleQuoted(testPrompt)
	var agentLine string
	switch agentKind {
	case "codex_cli":
		agentLine = fmt.Sprintf("codex exec -c model_provider=minimax -c model=%s --skip-git-repo-check --dangerously-bypass-approvals-and-sandbox \"%s\"", model, testPrompt)
	case "claude_cli":
		agentLine = fmt.Sprintf("claude -p \"%s\" --model %s --dangerously-skip-permissions", testPrompt, model)
	default:
		return "", fmt.Errorf("coordinator: unsupported test agent kind %q", agentKind)
	}
	script := "set -e\n" +
		"cd repo\n" +
		agentLine + "\n" +
		"echo REPO_TESTS_DONE=" + repoFullName + ":" + attemptID
	return "bash -c '" + script + "'", nil
}

// ReserveForTask registers the worker, one launch-verified attempt bound to
// the task's real issue, and the pending agent run whose command the executor
// claims. The attempt is inserted directly in launch_verified: the
// coordinator is the formal state writer (0020), the worker row it points at
// was just upserted with a live heartbeat, and the claim JOIN only matches
// launch_verified/running attempts.
func (l *coordinatorLedger) ReserveForTask(ctx context.Context, workerID, taskID, agentKind, title, instruction string) (string, error) {
	if _, err := l.pool.Exec(ctx, `INSERT INTO repomesh_execution.workers (id, host, kind, heartbeat_at)
		VALUES ($1,'coordinator','host_executor', clock_timestamp())
		ON CONFLICT (id) DO UPDATE SET heartbeat_at=clock_timestamp(), retired_at=NULL`, workerID); err != nil {
		return "", fmt.Errorf("coordinator: worker register failed: %w", err)
	}
	attemptID, err := newRunID("att_dag_")
	if err != nil {
		return "", err
	}
	// One short transaction binds everything: the task's real project/issue/
	// pinned-revision/repository feed the attempt and the command (the FK and
	// the configuration-match trigger both check real rows, so unresolved
	// placeholders abort here and dispatch defers to the next tick), and the
	// pending agent run commits in the same transaction so the executor can
	// never observe a half dispatch.
	tx, err := l.pool.Begin(ctx)
	if err != nil {
		return "", fmt.Errorf("coordinator: reserve begin failed: %w", err)
	}
	defer tx.Rollback(ctx)
	var projectID, issueID, revision, repoFullName, issueTitle, configuredKind, configuredModel string
	err = tx.QueryRow(ctx, `SELECT t.project_id::text, t.source_ref->>'issueId', i.initial_configuration_revision,
		       t.repository_id, i.title
		       , COALESCE(a.agent_kind, ''), COALESCE(a.model, '')
		FROM public.tasks t
		JOIN repomesh_issues.issues i ON i.project_id = t.project_id::text AND i.id = t.source_ref->>'issueId'
		LEFT JOIN repomesh_projects.agent_settings a ON a.project_id = t.project_id::text
		WHERE t.id::text=$1 LIMIT 1`, taskID).
		Scan(&projectID, &issueID, &revision, &repoFullName, &issueTitle, &configuredKind, &configuredModel)
	if err != nil {
		return "", fmt.Errorf("coordinator: task %s is not linked to an issue (missing source_ref?)", taskID)
	}
	// 项目级智能体配置（/app/ 项目页保存）覆盖部署默认：CLI 种类与模型。
	if configuredKind == "codex_cli" || configuredKind == "claude_cli" {
		agentKind = configuredKind
	}
	if configuredModel == "" {
		configuredModel = "MiniMax-M2"
	}
	command, err := buildAgentCommand(agentKind, configuredModel, instruction, repoFullName, attemptID, issueTitle, issueID)
	if err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO repomesh_execution.attempts
		(id, project_id, issue_id, worker_id, configuration_revision, state, reservation_generation, launch_verified_at)
		VALUES ($1,$2,$3,$4,$5,'launch_verified',
		 COALESCE((SELECT MAX(reservation_generation)+1 FROM repomesh_execution.attempts WHERE project_id=$2 AND issue_id=$3), 1),
		 clock_timestamp())
		ON CONFLICT (project_id, issue_id, reservation_generation) DO NOTHING`,
		attemptID, projectID, issueID, workerID, revision); err != nil {
		return "", fmt.Errorf("coordinator: attempt insert failed: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO repomesh_execution.attempt_events
		(attempt_id, sequence, kind, source)
		VALUES ($1, 1, 'launch_verified', 'coordinator')
		ON CONFLICT DO NOTHING`, attemptID); err != nil {
		return "", fmt.Errorf("coordinator: attempt event insert failed: %w", err)
	}
	runID, err := newRunID("run_dag_")
	if err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO repomesh_execution.agent_runs
		(id, attempt_id, agent_kind, command, workspace, task_package_ref, state)
		VALUES ($1,$2,$3,$4,$5,$6,'pending')`,
		runID, attemptID, agentKind, command,
		"/opt/repomesh/workspaces/"+attemptID, taskID); err != nil {
		return "", fmt.Errorf("coordinator: agent run insert failed: %w", err)
	}
	// 双派工：测试 run 挂在**独立的 attempt** 上。
	// agent_runs_one_live_per_attempt 是 (attempt_id) 上 WHERE state IN
	// ('pending','running') 的部分唯一索引——同一 attempt 只容得下一条 live run。
	// 把两条 run 放同一 attempt 会被 23505 拒绝，且因为同一事务而让整次派工回滚
	// （连开发 run 一起消失，DAG 永远推不动）。这正是 DispatchDual 原本就建两个
	// attempt 的原因。
	// workspace 仍指向**开发 run 的工作区**：被交付的改动就在那个目录的 repo/ 下，
	// 测试 agent 因此看到真实产物，而不是一份重新克隆的干净树。
	// 两条 run 仍在同一事务插入，created_at 相同，所以 ClaimAgentLaunch 用
	// (agent_kind='test_agent') 作 tiebreaker 保证先开发后测试。
	// agent_kind='test_agent' 已由 agent_runs_agent_kind_check 允许，无需迁移。
	testAttemptID, err := newRunID("att_dag_")
	if err != nil {
		return "", err
	}
	// reservation_generation 取 MAX+1：开发 attempt 刚在同一事务里插入，因此测试
	// attempt 会拿到 +1，不会撞 (project_id,issue_id,reservation_generation) 唯一键
	// （撞了会 DO NOTHING 静默不插，随后 agent_runs 的外键直接失败）。
	if _, err := tx.Exec(ctx, `INSERT INTO repomesh_execution.attempts
		(id, project_id, issue_id, worker_id, configuration_revision, state, reservation_generation, launch_verified_at)
		VALUES ($1,$2,$3,$4,$5,'launch_verified',
		 COALESCE((SELECT MAX(reservation_generation)+1 FROM repomesh_execution.attempts WHERE project_id=$2 AND issue_id=$3), 1),
		 clock_timestamp())
		ON CONFLICT (project_id, issue_id, reservation_generation) DO NOTHING`,
		testAttemptID, projectID, issueID, workerID, revision); err != nil {
		return "", fmt.Errorf("coordinator: test attempt insert failed: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO repomesh_execution.attempt_events
		(attempt_id, sequence, kind, source)
		VALUES ($1, 1, 'launch_verified', 'coordinator')
		ON CONFLICT DO NOTHING`, testAttemptID); err != nil {
		return "", fmt.Errorf("coordinator: test attempt event insert failed: %w", err)
	}
	testRunID, err := newRunID("run_test_")
	if err != nil {
		return "", err
	}
	testCommand, err := buildTestCommand(agentKind, configuredModel, instruction, repoFullName, attemptID, issueTitle)
	if err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO repomesh_execution.agent_runs
		(id, attempt_id, agent_kind, command, workspace, task_package_ref, state)
		VALUES ($1,$2,'test_agent',$3,$4,$5,'pending')`,
		testRunID, testAttemptID, testCommand,
		"/opt/repomesh/workspaces/"+attemptID, taskID); err != nil {
		return "", fmt.Errorf("coordinator: test run insert failed: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("coordinator: reserve commit failed: %w", err)
	}
	return attemptID, nil
}
