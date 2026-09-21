package execution

import (
	"context"
	"encoding/json"
	"fmt"
)

// maxDevAttempts 是一条任务允许被自动重派的开发 run 总数（含首次）。
//
// 取 3 的来由：执行面失败大多是**一次性的**（部署重启把在跑的 agent 杀掉、
// 上游仓库瞬时不可用），重来一次通常就过；连撞三次还没产出，再自动重试只会
// 白烧额度与模型调用，该停下来让人看。到顶后任务停在 'failed' 并带上原因。
const maxDevAttempts = 3

// TestEvidenceFile 是测试 agent 必须写出的证据文件名（工作区根下）。
//
// 派发端（coordinator 的 buildTestCommand）把它写进提示词，收端
// （host-executor 的 recordDeliveryFacts）按同名读回 —— 两边共用这一个常量，
// 免得改名时只改了一头、另一头静默读不到（那样测试记录会悄悄变空）。
const TestEvidenceFile = "test-evidence.json"

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
	//
	// 2026-09-20 线上实测：这里此前**不看退出码**——任何 run 一退出就把任务置成
	// blocked，界面于是写着「执行者已跑完并把任务交回」。而实测那批里 4 条开发
	// run 有 3 条是 exit 22 / 被部署重启杀掉的 killed -1，它们根本没产出、没开 PR，
	// 交付列车上只剩一节有 PR 的车厢。现在按 run 的真实结局分流：
	//   · 开发 run 成功退出（exit 0 且非被杀）→ blocked（经理门）；
	//   · 开发 run 失败/被杀 → 有重派额度就回 pending 并放回 plan_steps='ready'
	//     （否则调度器只挑 ready 的步，任务永远没人派），额度用完停在 failed；
	//   · 测试 run 是观察者，绝不改任务状态。
	var agentKind, taskRef string
	if err := s.pool.QueryRow(ctx, `SELECT agent_kind, COALESCE(task_package_ref,'')
		FROM repomesh_execution.agent_runs WHERE id=$1`, runID).Scan(&agentKind, &taskRef); err != nil {
		return unavailable()
	}
	if taskRef != "" && agentKind != "test_agent" && agentKind != "review_agent" {
		if exitCode == 0 && !killed {
			// 2026-09-21 用户实测：30 条 blocked 任务的 result_summary **全是空的** ——
			// 界面因此只能说"卡住了"，说不出为什么。原因就在这里：下面失败那条路径
			// 写了 result_summary，而这条**成功**路径只改状态、不留一句话。
			// 留痕不是装饰：经理门要批的是"执行者跑完了什么"，这句话就是那件事本身。
			summary := fmt.Sprintf(
				"开发 run 正常退出（exit=0，未被杀），任务交回经理门待批。run=%s", runID)
			if _, err := s.pool.Exec(ctx, `UPDATE public.tasks SET status='blocked', result_summary=$2
				WHERE id::text=$1 AND status='running'`, taskRef, summary); err != nil {
				return unavailable()
			}
		} else {
			var prior int
			if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM repomesh_execution.agent_runs
				WHERE task_package_ref=$1 AND agent_kind NOT IN ('test_agent','review_agent')`, taskRef).Scan(&prior); err != nil {
				return unavailable()
			}
			next := "failed"
			if prior < maxDevAttempts {
				next = "pending"
			}
			reason := fmt.Sprintf("执行未成功（exit=%d killed=%t），未产出可交付的改动", exitCode, killed)
			if next == "pending" {
				reason += "；已自动重派（第 " + fmt.Sprint(prior+1) + " / " + fmt.Sprint(maxDevAttempts) + " 次）"
			} else {
				reason += "；自动重派已用满 " + fmt.Sprint(maxDevAttempts) + " 次，需要人工介入"
			}
			if _, err := s.pool.Exec(ctx, `UPDATE public.tasks SET status=$2, result_summary=$3
				WHERE id::text=$1 AND status='running'`, taskRef, next, reason); err != nil {
				return unavailable()
			}
			if next == "pending" {
				if _, err := s.pool.Exec(ctx, `UPDATE public.plan_steps s SET status='ready'
					FROM public.tasks t
					WHERE s.plan_id = t.plan_id AND s.content = t.title
					  AND t.id::text=$1 AND s.status='dispatched'`, taskRef); err != nil {
					return unavailable()
				}
			}
		}
	}
	// 派发台账收尾：agent 退出即这条 attempt 结束。
	//
	// 2026-09-20 实测：31 条 attempt **永远停在 running**（stopped_at / fail_reason
	// 全空，最早的挂在 9-18），而 running→stop_requested→stopped 那条路径**全仓
	// 没有任何调用方** —— 台账从此不再反映现实，worker 的 active_attempts 也只会
	// 加不会减（并发额度被永久占住，后续派发只能一直 defer）。
	// 取消/中止仍走 RequestStop→ConfirmStopped；这里补的是**正常结束**的收尾。
	return s.closeAttempt(ctx, runID)
}

// closeAttempt 是"这条 attempt 结束了"的**唯一**收尾点：把 attempt 置 stopped，
// 并把 worker 的并发计数还回去。
//
// 抽出来的理由：agent 退出（MarkAgentExited）和"进程丢了"（MarkAgentLost）都要走
// 这一步，两处各写一遍迟早只改一处 —— 而漏改的后果是 worker 的 active_attempts
// 只加不减，并发额度被永久占住，后续派发只能一直 defer（这个故障线上真实发生过）。
func (s *Service) closeAttempt(ctx context.Context, runID string) error {
	tag, err := s.pool.Exec(ctx, `UPDATE repomesh_execution.attempts a
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
		ORDER BY r.created_at, CASE r.agent_kind WHEN 'test_agent' THEN 1 WHEN 'review_agent' THEN 2 ELSE 0 END LIMIT 1 FOR UPDATE OF r`, workerID).
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
