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
func buildAgentCommand(agentKind, instruction, repoFullName, attemptID, issueTitle, issueID string) (string, error) {
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
		agentLine = fmt.Sprintf("codex exec -c model_provider=minimax -c model=MiniMax-M2 --skip-git-repo-check --dangerously-bypass-approvals-and-sandbox \"%s\"", prompt)
	case "claude_cli":
		agentLine = fmt.Sprintf("claude -p \"%s\" --dangerously-skip-permissions", prompt)
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
	var projectID, issueID, revision, repoFullName, issueTitle string
	err = tx.QueryRow(ctx, `SELECT t.project_id::text, t.source_ref->>'issueId', i.initial_configuration_revision,
		       t.repository_id, i.title
		FROM public.tasks t
		JOIN repomesh_issues.issues i ON i.project_id = t.project_id::text AND i.id = t.source_ref->>'issueId'
		WHERE t.id::text=$1 LIMIT 1`, taskID).
		Scan(&projectID, &issueID, &revision, &repoFullName, &issueTitle)
	if err != nil {
		return "", fmt.Errorf("coordinator: task %s is not linked to an issue (missing source_ref?)", taskID)
	}
	command, err := buildAgentCommand(agentKind, instruction, repoFullName, attemptID, issueTitle, issueID)
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
	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("coordinator: reserve commit failed: %w", err)
	}
	return attemptID, nil
}
