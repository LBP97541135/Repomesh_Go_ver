package execution

import (
	"context"
	"encoding/json"
)

// AgentRunCommand launches one coding agent process inside an attempt.
type AgentRunCommand struct {
	AttemptID      string
	AgentKind      string // claude_cli | codex_cli
	Command        string
	Workspace      string
	TaskPackageRef string
	// RepoFullName 是本次要交付的仓库（owner/name），executor 据此**现场铸**
	// 该仓库的 installation token（C 修复）。测试 run 为空。
	RepoFullName string
}

// AgentRun records one agent process lifecycle.
type AgentRun struct {
	ID        string
	AttemptID string
	State     string
}

// StartAgentRun registers the intent to launch an agent and returns the run
// id; the executor flips it to running with the pid once the process exists.
func (s *Service) StartAgentRun(ctx context.Context, command AgentRunCommand) (AgentRun, error) {
	if command.AgentKind != "claude_cli" && command.AgentKind != "codex_cli" {
		return AgentRun{}, failure(422, "VALIDATION_FAILED")
	}
	if command.AttemptID == "" || command.Command == "" || command.Workspace == "" || command.TaskPackageRef == "" {
		return AgentRun{}, failure(422, "VALIDATION_FAILED")
	}
	// Attempt must be launch-verified or running: launch is never allowed on
	// an unverified reservation (ADR-0017 re-verify before launch).
	var state string
	if err := s.pool.QueryRow(ctx, `SELECT state FROM repomesh_execution.attempts WHERE id=$1`,
		command.AttemptID).Scan(&state); err != nil {
		return AgentRun{}, failure(404, "RESOURCE_NOT_FOUND")
	}
	if state != "launch_verified" && state != "running" {
		return AgentRun{}, failure(409, "LAUNCH_CONTEXT_CHANGED")
	}
	runID, err := newID("run_")
	if err != nil {
		return AgentRun{}, err
	}
	if _, err := s.pool.Exec(ctx, `INSERT INTO repomesh_execution.agent_runs
		(id, attempt_id, agent_kind, command, workspace, task_package_ref, state)
		VALUES ($1,$2,$3,$4,$5,$6,'pending')`,
		runID, command.AttemptID, command.AgentKind, command.Command, command.Workspace, command.TaskPackageRef); err != nil {
		return AgentRun{}, unavailable()
	}
	return AgentRun{ID: runID, AttemptID: command.AttemptID, State: "pending"}, nil
}

// MarkAgentRunning records the live process identity.
func (s *Service) MarkAgentRunning(ctx context.Context, runID string, pid int64) error {
	tag, err := s.pool.Exec(ctx, `UPDATE repomesh_execution.agent_runs
		SET state='running', pid=$2, started_at=clock_timestamp() WHERE id=$1 AND state='pending'`, runID, pid)
	if err != nil {
		return unavailable()
	}
	if tag.RowsAffected() != 1 {
		return failure(409, "AGENT_CONTEXT_CHANGED")
	}
	_, err = s.pool.Exec(ctx, `INSERT INTO repomesh_execution.attempt_events (attempt_id, sequence, kind, source, payload)
		SELECT attempt_id, (SELECT COALESCE(MAX(e.sequence),0)+1 FROM repomesh_execution.attempt_events e WHERE e.attempt_id = agent_runs.attempt_id),
		'agent_started', 'agent', jsonb_build_object('runId',$1::text,'pid',$2::bigint) FROM repomesh_execution.agent_runs WHERE id=$1::text`,
		runID, pid)
	if err != nil {
		return unavailable()
	}
	return nil
}

// MarkAgentExited records the terminal observation; exit evidence is the
// delivery-traceable outcome of the run.
func (s *Service) MarkAgentExited(ctx context.Context, runID string, exitCode int, killed bool, sessionRef string) error {
	state := "exited"
	if killed {
		state = "killed"
	}
	tag, err := s.pool.Exec(ctx, `UPDATE repomesh_execution.agent_runs
		SET state=$2, exit_code=$3, exited_at=clock_timestamp(), session_ref=NULLIF($4,'')
		WHERE id=$1 AND state='running'`, runID, state, exitCode, sessionRef)
	if err != nil {
		return unavailable()
	}
	if tag.RowsAffected() != 1 {
		return failure(409, "AGENT_CONTEXT_CHANGED")
	}
	kind := "agent_exited"
	payload := map[string]any{"runId": runID, "exitCode": exitCode, "killed": killed}
	if sessionRef != "" {
		payload["sessionRef"] = sessionRef
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		encoded = []byte("{}")
	}
	_, err = s.pool.Exec(ctx, `INSERT INTO repomesh_execution.attempt_events (attempt_id, sequence, kind, source, payload)
		SELECT attempt_id, (SELECT COALESCE(MAX(e.sequence),0)+1 FROM repomesh_execution.attempt_events e WHERE e.attempt_id = agent_runs.attempt_id),
		$2, 'agent', $3::jsonb FROM repomesh_execution.agent_runs WHERE id=$1::text`,
		runID, kind, encoded)
	if err != nil {
		return unavailable()
	}
	// C5 fix: an exited agent ends the running phase — the bound task must
	// reach the manager gate (blocked) or Approve/Reject can never act on it.
	// task_package_ref carries the public.tasks id for dispatch-created runs.
	_, err = s.pool.Exec(ctx, `UPDATE public.tasks SET status='blocked'
		WHERE id::text = (SELECT task_package_ref FROM repomesh_execution.agent_runs WHERE id=$1)
		  AND status='running'`, runID)
	if err != nil {
		return unavailable()
	}
	// 派发台账收尾：agent 退出即这条 attempt 结束。
	//
	// 2026-09-20 实测：31 条 attempt **永远停在 running**（stopped_at / fail_reason
	// 全空，最早的挂在 9-18），而 running→stop_requested→stopped 那条路径**全仓
	// 没有任何调用方** —— 台账从此不再反映现实，worker 的 active_attempts 也只会
	// 加不会减（并发额度被永久占住，后续派发只能一直 defer）。
	// 取消/中止仍走 RequestStop→ConfirmStopped；这里补的是**正常结束**的收尾。
	tag, err = s.pool.Exec(ctx, `UPDATE repomesh_execution.attempts a
		SET state='stopped', stopped_at=clock_timestamp()
		WHERE a.state='running'
		  AND a.id = (SELECT r.attempt_id FROM repomesh_execution.agent_runs r WHERE r.id=$1)`, runID)
	if err != nil {
		return unavailable()
	}
	if tag.RowsAffected() == 1 {
		if _, err := s.pool.Exec(ctx, `UPDATE repomesh_execution.workers w
			SET active_attempts = GREATEST(w.active_attempts - 1, 0)
			WHERE w.id = (SELECT a.worker_id FROM repomesh_execution.attempts a
				JOIN repomesh_execution.agent_runs r ON r.attempt_id = a.id WHERE r.id=$1)`, runID); err != nil {
			return unavailable()
		}
	}
	return nil
}

// MarkAgentLaunchFailed records a process that never started.
func (s *Service) MarkAgentLaunchFailed(ctx context.Context, runID string) error {
	tag, err := s.pool.Exec(ctx, `UPDATE repomesh_execution.agent_runs
		SET state='failed_launch' WHERE id=$1 AND state IN ('pending','running')`, runID)
	if err != nil {
		return unavailable()
	}
	if tag.RowsAffected() != 1 {
		return failure(409, "AGENT_CONTEXT_CHANGED")
	}
	return nil
}

// ClaimAgentLaunch atomically claims one pending agent run for this worker's
// attempt and moves the attempt to running. The executor polls this.
func (s *Service) ClaimAgentLaunch(ctx context.Context, workerID string) (AgentRunCommand, string, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return AgentRunCommand{}, "", unavailable()
	}
	defer rollback(tx)
	var runID, attemptID, kind, command, workspace, packageRef, repoFullName string
	// 双派工后同一 attempt 上挂着开发 run 与 test_agent run，二者在同一事务插入，
	// created_at（now() = 事务开始时刻）完全相同，只按 created_at 排序时先后是不确定的
	// ——测试可能在开发产出之前就被认领。加 (agent_kind='test_agent') 作 tiebreaker：
	// 同一时间戳内开发 run 恒排在测试 run 之前，跨 attempt 仍按插入顺序。
	err = tx.QueryRow(ctx, `SELECT r.id, r.attempt_id, r.agent_kind, r.command, r.workspace, r.task_package_ref, r.repo_full_name
		FROM repomesh_execution.agent_runs r
		JOIN repomesh_execution.attempts a ON a.id = r.attempt_id
		WHERE a.worker_id=$1 AND r.state='pending' AND a.state IN ('launch_verified','running')
		ORDER BY r.created_at, (r.agent_kind = 'test_agent') LIMIT 1 FOR UPDATE OF r`, workerID).
		Scan(&runID, &attemptID, &kind, &command, &workspace, &packageRef, &repoFullName)
	if err != nil {
		if err.Error() == "no rows in result set" {
			return AgentRunCommand{}, "", nil
		}
		return AgentRunCommand{}, "", unavailable()
	}
	if _, err := tx.Exec(ctx, `UPDATE repomesh_execution.attempts SET state='running'
		WHERE id=$1 AND state='launch_verified'`, attemptID); err != nil {
		return AgentRunCommand{}, "", unavailable()
	}
	if err := tx.Commit(ctx); err != nil {
		return AgentRunCommand{}, "", unavailable()
	}
	return AgentRunCommand{AttemptID: attemptID, AgentKind: kind, Command: command, Workspace: workspace, TaskPackageRef: packageRef, RepoFullName: repoFullName}, runID, nil
}
