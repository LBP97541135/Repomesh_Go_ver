package main

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"repomesh.local/repomesh/internal/discovery"
	"repomesh.local/repomesh/internal/spec"
)

// specChangeApplier 实现 web.SpecChangeApplier：把"人批准了规格变更"落成两件真事 ——
// **规格升版**（internal/spec）与**登记重排 v2**（internal/discovery 的第 6 步意图）。
//
// 这是 A2 回路里唯一能改 spec 生效的动作。agent 只能"提"（host executor 收产物落
// 审核台），人在审核台上按了批准，才走到这里。
type specChangeApplier struct {
	specs     *spec.Service
	discovery *discovery.Service
	pool      *pgxpool.Pool
}

func (a specChangeApplier) ApplyApprovedChange(ctx context.Context, projectID, actor, reviewID, evidenceVersion string) (string, error) {
	applied, err := a.specs.ApproveChange(ctx, projectID, actor, evidenceVersion)
	if err != nil {
		return "", err
	}
	// 审核单里带着 issue_id（host executor 落单时写进 request_content）——规格换了，
	// 该 issue 的计划要重排。找不到 issue/计划就**如实说明没登记重排**，不假装排了。
	var issueID string
	if err := a.pool.QueryRow(ctx,
		`SELECT COALESCE(request_content->>'issue_id','') FROM public.review_requests WHERE id=$1::uuid`,
		reviewID).Scan(&issueID); err != nil {
		return "", fmt.Errorf("spec-change: 审核单读取失败：%w", err)
	}
	if issueID == "" {
		return fmt.Sprintf("规格 %s 已升到 v%d；该审核单没有关联 issue，未登记重排",
			applied.Repository, applied.Version), nil
	}
	var planID string
	err = a.pool.QueryRow(ctx,
		`SELECT id::text FROM public.plans WHERE issue_id=$1 LIMIT 1`, issueID).Scan(&planID)
	if err != nil {
		return fmt.Sprintf("规格 %s 已升到 v%d；该 issue 还没有计划，未登记重排",
			applied.Repository, applied.Version), nil
	}
	if err := a.discovery.EnqueueReplan(ctx, planID, reviewID, []string{applied.Repository}); err != nil {
		return "", fmt.Errorf("spec-change: 登记重排失败：%w", err)
	}
	return fmt.Sprintf("规格 %s 已升到 v%d，并已登记重排 v2（等 Leader 产出）",
		applied.Repository, applied.Version), nil
}
