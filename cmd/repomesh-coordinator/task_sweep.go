package main

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"repomesh.local/repomesh/internal/execution"
)

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
