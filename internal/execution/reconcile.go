package execution

import (
	"context"
	"encoding/json"
	"fmt"
)

// reconcile.go 收尾"进程已经不在、台账还停在 running"的 agent run。
//
// 为什么必须有它（2026-09-21 线上实测，用户报"任务一直执行不完"）：
//
//	host-executor 监督子进程的方式是**在本进程里 `cmd.Wait()`**（见
//	cmd/repomesh-host-executor/agent_launch_unix.go）。于是只要 executor 自己被
//	重启（部署、systemd restart、OOM），那个等待就**随进程一起消失**：
//	  · repomesh_execution.agent_runs.state 永远停在 'running'，exit_code 为空；
//	  · 任务永远停在 'running' —— 因为"任务交回经理门"这一步只写在退出路径上；
//	  · attempt 也永远停在 'running'，worker 的 active_attempts 只加不减，
//	    并发额度被占死，后续派发一直 defer。
//
//	实测那条：run_dag_c4aba7a93a72d817c5d0（saleor-app-template 的开发 run）
//	11:05:48 起跑，pid 2073721 早已不存在，而 2 小时后台账还写着 running，
//	任务 48573c11 也就一直挂着。库里同时有 36 条 attempt 停在这个状态。
//
// 修法：启动时（以及之后每 5 分钟）扫一遍**本 worker 名下**停在 running 的 run，
// 用 pid 探活；进程真没了就按**未知结局**收尾 —— 不猜它成功，也不猜它失败：
//   · agent_runs.state='lost'，exit_code 留空（我们确实不知道），exited_at 记上；
//   · 写一条 attempt_event 说明原委，证据链不断；
//   · attempt 收尾、并发额度还回去；
//   · 开发 run 的任务**按"未交付"处理**：还有重派额度就回 pending 重来，
//     额度用满停在 failed，两种情况都把"退出码未知"写进 result_summary。
//     绝不因为"进程可能干完了"就替它宣布成功 —— 线上那条的 workspace 里
//     只有未提交的文件，没有 commit、没有 PR，交付根本没发生。

// LostRun 是一条待收尾的孤儿 run。
type LostRun struct {
	RunID     string
	AttemptID string
	AgentKind string
	PID       *int64
	TaskRef   string
	RepoName  string
	Workspace string
	StartedAt string
}

// ListLostCandidateRuns 列出本 worker 名下、台账停在 running 的 run。
//
// 只按 **worker_id** 过滤：别的 worker 名下的 run 由别的 executor 负责，
// 越界去收尾会把别人正在跑的活判死。
func (s *Service) ListLostCandidateRuns(ctx context.Context, workerID string) ([]LostRun, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT r.id, r.attempt_id, r.agent_kind, r.pid, COALESCE(r.task_package_ref,''),
		       COALESCE(r.repo_full_name,''), r.workspace,
		       COALESCE(to_char(r.started_at, 'YYYY-MM-DD"T"HH24:MI:SSOF'), '')
		FROM repomesh_execution.agent_runs r
		JOIN repomesh_execution.attempts a ON a.id = r.attempt_id
		WHERE a.worker_id = $1 AND r.state = 'running'`, workerID)
	if err != nil {
		return nil, unavailable()
	}
	defer rows.Close()
	var out []LostRun
	for rows.Next() {
		run := LostRun{}
		if err := rows.Scan(&run.RunID, &run.AttemptID, &run.AgentKind, &run.PID,
			&run.TaskRef, &run.RepoName, &run.Workspace, &run.StartedAt); err != nil {
			return nil, unavailable()
		}
		out = append(out, run)
	}
	return out, rows.Err()
}

// ReconcileLostRuns 扫一遍并收尾"进程已不存在"的 run，返回收尾条数。
//
// alive 由调用方注入（unix 下是 kill(pid, 0)），这样判定只写一次、平台差异只在一个文件里。
// pid 为空（进程还没起来就重启了）同样按丢失处理。
//
// 注意判定方向是**保守的**：只有"探活明确说不存在"才收尾。pid 被复用给新进程时
// 探活会说"活着"，我们于是什么都不做（宁可漏收，不可错杀）。
func (s *Service) ReconcileLostRuns(ctx context.Context, workerID string, alive func(pid int64) bool) (int, error) {
	candidates, err := s.ListLostCandidateRuns(ctx, workerID)
	if err != nil {
		return 0, err
	}
	closed := 0
	for _, run := range candidates {
		if run.PID != nil && alive(*run.PID) {
			continue
		}
		reason := fmt.Sprintf("agent 进程已不存在（pid=%s，host-executor 重启后等待丢失），退出码未知",
			pidText(run.PID))
		if err := s.MarkAgentLost(ctx, run.RunID, reason); err != nil {
			return closed, err
		}
		closed++
	}
	return closed, nil
}

func pidText(pid *int64) string {
	if pid == nil {
		return "未记录"
	}
	return fmt.Sprintf("%d", *pid)
}

// MarkAgentLost 把一条 running 的 run 按**未知结局**收尾。
//
// 与 MarkAgentExited 的区别是它**不编造退出码**：exit_code 保持 NULL，
// state 用 'lost' 而不是 'exited'/'killed'。下游按 state 读事实的地方
// （交付记账、评估读面）因此能一眼看出"这条 run 的结局未知"。
func (s *Service) MarkAgentLost(ctx context.Context, runID, reason string) error {
	tag, err := s.pool.Exec(ctx, `UPDATE repomesh_execution.agent_runs
		SET state='lost', exited_at=clock_timestamp()
		WHERE id=$1 AND state='running'`, runID)
	if err != nil {
		return unavailable()
	}
	if tag.RowsAffected() != 1 {
		// 已经有人收尾过了（正常退出路径先到）—— 不是错误，什么都不做。
		return nil
	}
	payload, err := json.Marshal(map[string]any{"runId": runID, "reason": reason, "exitCode": nil})
	if err != nil {
		payload = []byte("{}")
	}
	if _, err := s.pool.Exec(ctx, `INSERT INTO repomesh_execution.attempt_events (attempt_id, sequence, kind, source, payload)
		SELECT attempt_id, (SELECT COALESCE(MAX(e.sequence),0)+1 FROM repomesh_execution.attempt_events e WHERE e.attempt_id = agent_runs.attempt_id),
		'agent_lost', 'host_executor', $2::jsonb FROM repomesh_execution.agent_runs WHERE id=$1::text`,
		runID, string(payload)); err != nil {
		return unavailable()
	}
	var agentKind, taskRef string
	if err := s.pool.QueryRow(ctx, `SELECT agent_kind, COALESCE(task_package_ref,'')
		FROM repomesh_execution.agent_runs WHERE id=$1`, runID).Scan(&agentKind, &taskRef); err != nil {
		return unavailable()
	}
	// 测试/审查 run 是观察者，绝不改任务状态（与退出路径同一规矩）——
	// 它们丢了只意味着"这批证据不会再来"，任务该停在经理门由人看。
	if taskRef != "" && agentKind != "test_agent" && agentKind != "review_agent" {
		if err := s.advanceTaskAfterLostRun(ctx, taskRef, runID); err != nil {
			return err
		}
	}
	return s.closeAttempt(ctx, runID)
}

// advanceTaskAfterLostRun 把"开发 run 结局未知"如实写进任务。
//
// 2026-09-22 线上实测（用户："看看有没有卡点"）—— 这里此前把**进程丢失**和
// **执行失败**当成同一件事：都吃掉一次重派额度。可"进程丢失"是 systemd 重启
// executor 造成的（默认 KillMode=control-group，会把跑在 executor cgroup 里的 agent
// 一起杀掉），**不是这条任务跑失败了**。一条任务被部署撞三次就被写成"自动重派已用满，
// 需要人工介入"，而它一次真正的尝试都没做过 —— 台账里于是堆满假卡点。
//
// 现在：进程丢失**不计入重派额度**，直接回 pending 重排队；但仍受绝对上限
// （maxDevRunsAbsolute）约束，免得环境一直丢进程时无限重派。
func (s *Service) advanceTaskAfterLostRun(ctx context.Context, taskRef, runID string) error {
	verdicts, total, err := s.devAttemptBudget(ctx, taskRef)
	if err != nil {
		return err
	}
	next := "failed"
	reason := fmt.Sprintf("执行进程丢失、退出码未知（host-executor 重启），未产出可交付的改动。run=%s", runID)
	if verdicts < maxDevAttempts && total < maxDevRunsAbsolute {
		next = "pending"
		reason += "；本次**不计入**重派额度（是部署重启把进程带走的，不是这条任务跑失败），已重新排队"
	} else {
		reason += "；重派已到上限（已给出结论的尝试 " + fmt.Sprint(verdicts) + " / " +
			fmt.Sprint(maxDevAttempts) + "，含进程丢失共 " + fmt.Sprint(total) + " 次），需要人工介入"
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
	return nil
}
