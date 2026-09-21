package main

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"repomesh.local/repomesh/internal/execution"
	"repomesh.local/repomesh/internal/roomnotice"
	"repomesh.local/repomesh/internal/scm"
	"repomesh.local/repomesh/internal/tasks"
)

// sweepCloseDeliveredIssues 把**交付已全部合入主分支**的 issue 收口（归档）。
//
// 为什么要有它（2026-09-21 用户原话："都已经合并完成了，为什么没有关闭 issues"）：
// 这套模型里 issue 的"关闭"就是归档（archived_at），而归档**只有人工入口**
// （POST /api/issues/{id}/archive，界面上那个「归档」按钮）—— 全仓没有任何地方
// 在交付完成后自动收口。于是 PR 全合完了，列表里它还是 Open，得人一个个去点。
//
// 判定刻意严格，四个条件全满足才关（缺一个都说明"这条需求还没走完"）：
//  1. 还没归档、也没被清除；
//  2. **有**计划（没有任何计划的 issue 不是"交付完成"，是"还没开始"）；
//  3. 计划下**所有任务都 done**（superseded 不算 —— 被新版本取代的步不该拦着收口）；
//  4. 所有 change set **都 merged**，且**至少有一条** —— 没有 PR 的"完成"不算交付，
//     那多半是任务被人工判过了但根本没开 PR，收口会掩盖问题。
//
// 关闭是**软动作**：只打 archived_at，一行数据不删，随时能回看。
// 房间里留一条消息说明"为什么它自己关了" —— 状态自己变了却不说原因，人会以为出错了。
func sweepCloseDeliveredIssues(ctx context.Context, pool *pgxpool.Pool, rooms *roomnotice.Notifier) (int, error) {
	rows, err := pool.Query(ctx, `
		SELECT i.id
		FROM repomesh_issues.issues i
		WHERE i.archived_at IS NULL AND i.removed_at IS NULL
		  AND EXISTS (SELECT 1 FROM public.plans p WHERE p.issue_id = i.id)
		  AND NOT EXISTS (
		      SELECT 1 FROM public.tasks t
		      JOIN public.plans p ON p.id = t.plan_id
		      WHERE p.issue_id = i.id AND t.status NOT IN ('done', 'superseded'))
		  AND NOT EXISTS (
		      SELECT 1 FROM public.change_sets cs
		      JOIN public.tasks t ON t.id = cs.task_id
		      JOIN public.plans p ON p.id = t.plan_id
		      WHERE p.issue_id = i.id AND cs.status <> 'merged')
		  AND EXISTS (
		      SELECT 1 FROM public.change_sets cs
		      JOIN public.tasks t ON t.id = cs.task_id
		      JOIN public.plans p ON p.id = t.plan_id
		      WHERE p.issue_id = i.id AND cs.status = 'merged')`)
	if err != nil {
		return 0, fmt.Errorf("issue close: 巡检查询失败: %w", err)
	}
	var candidates []string
	for rows.Next() {
		var issueID string
		if err := rows.Scan(&issueID); err != nil {
			rows.Close()
			return 0, fmt.Errorf("issue close: 巡检读取失败: %w", err)
		}
		candidates = append(candidates, issueID)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}

	closed := 0
	for _, issueID := range candidates {
		tag, err := pool.Exec(ctx, `UPDATE repomesh_issues.issues
			SET archived_at = COALESCE(archived_at, now())
			WHERE id=$1 AND archived_at IS NULL AND removed_at IS NULL`, issueID)
		if err != nil {
			return closed, fmt.Errorf("issue close: 归档失败 issue=%s: %w", issueID, err)
		}
		if tag.RowsAffected() != 1 {
			continue
		}
		if rooms != nil {
			rooms.Notify(ctx, issueID, "issue-closed:"+issueID,
				"【RepoMesh】本需求的交付已全部合入主分支，issue 已自动收口（归档）。"+
					"数据一行没删，点列表上的「已归档」就能回看。")
		}
		closed++
	}
	return closed, nil
}

// sweepAutoMergeDeliveredChanges 在**选了「自动合并」**的 issue 上，把闸门已开的 PR 合掉。
//
// 为什么要有它（2026-09-21 用户原话："我们能不能把 pr 合并也做出可选择项目，ai 自动
// 模式自动合并，也可以选择人工审核"）：合并是整条链上**唯一的外部副作用**（真动用户
// 仓库、真进主分支），所以它既不该是硬编码的"永远人工"，也不该是"永远自动"——
// 而应该是建单时的一个显式选择（迁移 0064 的 issues.merge_mode）。
//
// 这个巡检只处理 **merge_mode='auto'** 的行；缺省 manual 的一律不碰。
//
// 三道门一道都不省（全部由 scm.Merge 自己再校验一遍，这里只是先筛掉明显不满足的）：
//
//	· 必须有 PR；
//	· **交付闸门必须开着**（pushed / pr / ci / reviewed 四项）—— 闸门没开就等，不硬合；
//	· GitHub App 凭据必须可用（merger 为 nil 时如实报错，不假装合了）。
//
// 单条失败不拖累其它：一条合不动（比如 GitHub 拒绝）只记日志，继续下一条。
func sweepAutoMergeDeliveredChanges(ctx context.Context, pool *pgxpool.Pool, scmSvc *scm.Service, merger scm.PullMerger) (int, error) {
	if scmSvc == nil {
		return 0, nil
	}
	rows, err := pool.Query(ctx, `
		SELECT cs.id::text, t.project_id::text, COALESCE(cs.pr_url,'')
		FROM public.change_sets cs
		JOIN public.tasks t ON t.id = cs.task_id
		JOIN public.plans p ON p.id = t.plan_id
		JOIN repomesh_issues.issues i ON i.id = p.issue_id AND i.project_id = p.project_id::text
		WHERE i.merge_mode = 'auto'
		  AND cs.status <> 'merged'
		  AND COALESCE(cs.pr_url,'') <> ''`)
	if err != nil {
		return 0, fmt.Errorf("auto merge: 巡检查询失败: %w", err)
	}
	type deliverable struct {
		changeSetID string
		projectID   string
		prURL       string
	}
	var candidates []deliverable
	for rows.Next() {
		item := deliverable{}
		if err := rows.Scan(&item.changeSetID, &item.projectID, &item.prURL); err != nil {
			rows.Close()
			return 0, fmt.Errorf("auto merge: 巡检读取失败: %w", err)
		}
		candidates = append(candidates, item)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}

	merged := 0
	for _, item := range candidates {
		gate, err := scmSvc.Gate(ctx, item.changeSetID)
		if err != nil {
			slog.Warn("auto merge: 读闸门失败", "changeSet", item.changeSetID, "reason", err.Error())
			continue
		}
		if !gate.Open {
			// 闸门没开就等 —— 缺的那几项要由流水线真实事件补齐，不硬合。
			slog.Info("auto merge: 闸门未开，等待",
				"changeSet", item.changeSetID, "push", gate.Pushed, "pr", gate.PR,
				"ci", gate.CIPassed, "reviewed", gate.Reviewed)
			continue
		}
		outcome, err := scmSvc.Merge(ctx, item.projectID, item.changeSetID, autohostAgent, merger)
		if err != nil {
			slog.Warn("auto merge: 合并失败", "changeSet", item.changeSetID, "pr", item.prURL, "reason", err.Error())
			continue
		}
		if outcome.Merged {
			merged++
			slog.Info("auto merge: 已合并", "changeSet", item.changeSetID, "pr", item.prURL)
		}
	}
	return merged, nil
}

// sweepAutoApproveManagerGate 在「自动托管」的 issue 上**由 Leader 代行经理门审批**。
//
// 为什么需要它（2026-09-21 用户原话："worker 受阻这个问题，我都用了自动模式了，
// 应该让 leader 帮我审批的"）：自动托管（hitl_mode='ai'）此前只覆盖**发现链**的
// 那几道门（分档审批、生成计划、物化确认 —— 见 discovery_auto.go），
// 而 DAG 跑起来之后的**经理门**（任务置 blocked 等人点「通过」）压根不在它的
// 管辖范围里。于是"全自动"跑到每个任务末尾都会停下来等人 —— 用户看到的正是这个。
//
// 判定与处置：
//
//	· 只挑 status='blocked' 且**该 issue 是 ai 模式**的任务；hitl 模式一行不碰
//	  （那是"门等真人"的语义，替人做主比不做事更坏）；
//	· 审批走**与 HTTP 端点同一条服务方法**（tasks.ApproveStep），不另写一套状态迁移；
//	· 同时补记交付闸门的 review 那一项（与经理端点一样，fail-open：记账失败不回滚审批）。
//
// 不拿"自动"当放行的理由：result_summary 如实写明是谁批的、以及单点验收到底有没有过。
// 验收缺失/未过时**照样批**（闸门是人的判断，不该由这里替它下结论），但那句话会写在
// 任务上；真正的拦截留给交付闸门（pushed/pr/ci/reviewed 四项 fail-closed）。
func sweepAutoApproveManagerGate(ctx context.Context, pool *pgxpool.Pool, store *tasks.PostgresStore, scmSvc *scm.Service) (int, error) {
	rows, err := pool.Query(ctx, `
		SELECT t.id::text,
		       (SELECT count(*) FROM public.test_evidence e
		         WHERE e.task_id = t.id AND e.kind = 'task_single_point') AS evidence_rows,
		       COALESCE((SELECT bool_or(e.passed) FROM public.test_evidence e
		         WHERE e.task_id = t.id AND e.kind = 'task_single_point'), false) AS evidence_passed
		FROM public.tasks t
		JOIN public.plans p ON p.id = t.plan_id
		JOIN repomesh_issues.issues i ON i.id = p.issue_id AND i.project_id = p.project_id::text
		WHERE t.status = 'blocked' AND i.hitl_mode = 'ai'`)
	if err != nil {
		return 0, fmt.Errorf("autohost gate: 巡检查询失败: %w", err)
	}
	type gated struct {
		id     string
		rows   int
		passed bool
	}
	var candidates []gated
	for rows.Next() {
		item := gated{}
		if err := rows.Scan(&item.id, &item.rows, &item.passed); err != nil {
			rows.Close()
			return 0, fmt.Errorf("autohost gate: 巡检读取失败: %w", err)
		}
		candidates = append(candidates, item)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}

	approved := 0
	for _, item := range candidates {
		summary := "自动托管：Leader 代行经理门审批（本 issue 未开启人工参与审计）"
		switch {
		case item.passed:
			summary += "；单点验收已通过"
		case item.rows > 0:
			summary += "；已有单点验收记录但未通过 —— 交付闸门仍会拦，不会因此放行"
		default:
			summary += "；尚无单点验收记录 —— 交付闸门仍会拦，不会因此放行"
		}
		if err := store.ApproveStep(ctx, item.id, autohostAgent, summary); err != nil {
			return approved, fmt.Errorf("autohost gate: 代行审批失败 task=%s: %w", item.id, err)
		}
		if scmSvc != nil {
			if changeSetID, csErr := scmSvc.ChangeSetForTask(ctx, item.id); csErr == nil && changeSetID != "" {
				_ = scmSvc.RecordEvent(ctx, changeSetID, "review",
					`{"actor":"`+autohostAgent+`","decision":"approved","mode":"autohost"}`)
			}
		}
		approved++
	}
	return approved, nil
}

// task_sweep.go 收尾"任务标着在跑、却**没有任何在跑的 run**"的行。
//
// 为什么需要它（2026-09-21 线上实测）：任务 80c7e8b8（repomesh-e2e-api 的
// "验证满700免运费功能"）从 9-20 13:35 起就挂在 running，而它名下 4 个 run
// （2 个开发 run exit=1/killed、2 个测试 run exit=1）**全都已经终结**。
// 也就是说：没有任何东西在跑，任务却说自己在跑 —— 界面于是永远显示"进行中"，
// 经理门等不到它，计划也永远收不了尾。
//
// 与 run 级收尾（internal/execution/reconcile.go）的分工：
//
//	· run 级：run 还写着 running、进程却没了 —— 由 host-executor 扫（它才看得见 pid）；
//	· 任务级：run 都终结了、任务却没跟着走 —— 由协调器扫（任务状态是协调器的正式写入）。
//
// 判定刻意保守，只碰"确定没有人在跑"的行：
//
//	· 任务 status='running'；
//	· 它名下没有任何 state IN ('pending','running') 的 run；
//	· 且**至少有一个 run 的 exited_at 已经过了冷却期**（防止误伤刚派发、
//	  run 行还没落库的窗口 —— 那种情况一条 run 都没有，这里不碰）。
//
// 处置与退出路径同一套语义：还有重派额度就回 pending 并把计划步放回 ready
// （否则调度器永不挑它），额度用满停在 failed。两种都把事实写进 result_summary。
func sweepStaleRunningTasks(ctx context.Context, pool *pgxpool.Pool, idleFor time.Duration) (int, error) {
	rows, err := pool.Query(ctx, `
		SELECT t.id::text, t.title,
		       (SELECT count(*) FROM repomesh_execution.agent_runs r
		         WHERE r.task_package_ref = t.id::text
		           AND r.agent_kind NOT IN ('test_agent','review_agent')) AS dev_runs,
		       (SELECT max(r.exited_at) FROM repomesh_execution.agent_runs r
		         WHERE r.task_package_ref = t.id::text) AS last_exit
		FROM public.tasks t
		WHERE t.status = 'running'
		  AND NOT EXISTS (SELECT 1 FROM repomesh_execution.agent_runs r
		                   WHERE r.task_package_ref = t.id::text
		                     AND r.state IN ('pending','running'))
		  AND (SELECT max(r.exited_at) FROM repomesh_execution.agent_runs r
		        WHERE r.task_package_ref = t.id::text) < now() - $1::interval`,
		idleFor.String())
	if err != nil {
		return 0, fmt.Errorf("tasks: 巡检查询失败: %w", err)
	}
	type stale struct {
		id      string
		title   string
		devRuns int
	}
	var candidates []stale
	for rows.Next() {
		item := stale{}
		var lastExit *time.Time
		if err := rows.Scan(&item.id, &item.title, &item.devRuns, &lastExit); err != nil {
			rows.Close()
			return 0, fmt.Errorf("tasks: 巡检读取失败: %w", err)
		}
		candidates = append(candidates, item)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}

	closed := 0
	for _, item := range candidates {
		next, reason := "failed", ""
		if item.devRuns < execution.MaxDevAttempts {
			next = "pending"
			reason = fmt.Sprintf(
				"任务标着在跑，但它名下已经没有任何在跑的 run（最后一次结束于冷却期之前）；"+
					"已自动重派（第 %d / %d 次）", item.devRuns+1, execution.MaxDevAttempts)
		} else {
			reason = fmt.Sprintf(
				"任务标着在跑，但它名下已经没有任何在跑的 run，且自动重派已用满 %d 次，需要人工介入",
				execution.MaxDevAttempts)
		}
		tag, err := pool.Exec(ctx, `UPDATE public.tasks SET status=$2, result_summary=$3
			WHERE id::text=$1 AND status='running'`, item.id, next, reason)
		if err != nil {
			return closed, fmt.Errorf("tasks: 巡检写入失败: %w", err)
		}
		if tag.RowsAffected() != 1 {
			continue
		}
		if next == "pending" {
			if _, err := pool.Exec(ctx, `UPDATE public.plan_steps s SET status='ready'
				FROM public.tasks t
				WHERE s.plan_id = t.plan_id AND s.content = t.title
				  AND t.id::text=$1 AND s.status='dispatched'`, item.id); err != nil {
				return closed, fmt.Errorf("tasks: 巡检放回计划步失败: %w", err)
			}
		}
		closed++
	}
	return closed, nil
}
