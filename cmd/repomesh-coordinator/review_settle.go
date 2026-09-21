package main

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// settleDiscoveryMirrors 销掉「发现链已经走完、审核台那张镜像单却还挂着 pending」的行。
//
// 2026-09-21 用户报：issue 的「仓库范围分档待审批」明明已经审核通过，审核台里却
// 一直显示**待审**，要求「已经审核过的能自动关闭」。
//
// 根因是**两条审批路只销了一条**。审核台那张单是发现链人工步骤的**镜像登记**
// （`request_content->>'origin' = 'discovery'`，由 planning.go / discovery_routes.go
// 的 raiseReview 落），销单逻辑 `settleReview` 只写在
// `internal/web/discovery_routes.go` 的 HTTP handler 里 —— 那是**人在 issue 页面点
// 通过**那条路。而自动托管模式下，③ 审批与 ⑤ 物化是 **coordinator 自己**调的
// （discovery_auto.go 的 `a.service.Approval` / `a.service.Materialize`），
// 那条路从来不销单，镜像单于是永远停在 pending。
//
// 这里按**观察到的发现链状态**对账，而不是按"谁调了哪个函数"：
//   - approval 已经是 approved  → repository_scope 那张待审单没有存在理由
//   - materialization 落了库     → execution 那张同理
//
// 这样它对两条审批路都成立，也能自愈历史上已经卡住的行（`settleReview` 是 fail-open
// 的 `_ = ...`，写失败会静默留下这种行）。
//
// 幂等：只动 status='pending' 的行，重复跑没有副作用。issueID 传空串表示全量对账。
func settleDiscoveryMirrors(ctx context.Context, pool *pgxpool.Pool, issueID string) (int, error) {
	if pool == nil {
		return 0, nil
	}
	tag, err := pool.Exec(ctx, `
		UPDATE public.review_requests rr
		SET status = 'approved',
		    decided_at = now(),
		    updated_at = now(),
		    decision_note = COALESCE(NULLIF(rr.decision_note, ''),
		                             '发现链已完成该步骤，镜像登记自动销单')
		FROM repomesh_issues.issue_discoveries d
		WHERE rr.status = 'pending'
		  AND rr.request_content->>'origin' = 'discovery'
		  AND d.issue_id = rr.request_content->>'issue_id'
		  AND ($1 = '' OR d.issue_id = $1)
		  AND (
		        (rr.checkpoint = 'repository_scope' AND d.approval->>'state' = 'approved')
		     OR (rr.checkpoint = 'execution'
		         AND d.materialization IS NOT NULL
		         AND d.materialization <> 'null'::jsonb)
		      )`, issueID)
	if err != nil {
		return 0, fmt.Errorf("settle discovery mirrors: %w", err)
	}
	return int(tag.RowsAffected()), nil
}
