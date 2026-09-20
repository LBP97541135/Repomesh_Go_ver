package branchvalidation

import (
	"context"
	"fmt"
	"time"
)

// sweep.go 是 C③ 的环境回收治理（评委建议③）。
//
// 评委原话：企业接入多个项目后，临时数据库环境需要有明确的归属、权限、配额与清理
// 机制；建议记录分支创建、使用、清理失败与重试状态，避免测试资源长期遗留；正式控制面
// 可部署在生产库上，并按组织、项目管理数据访问；决赛可展示交付完成后的环境回收与
// 证据保留，以及**服务重启后未完成任务仍可继续处理**。
//
// 这里的关键设计：扫描状态**全部来自库**（待清理的行就在表里），所以进程重启不丢
// 进度 —— "重启后继续"不需要额外的内存态。回收只删**数据库分支本身**，验证证据
// （results、状态、失败原因）原样保留。

// SweepResult 是一轮回收扫描的结果。
type SweepResult struct {
	Scanned   int `json:"scanned"`
	Reclaimed int `json:"reclaimed"`
	Failed    int `json:"failed"`
}

// SweepCleanup 扫一轮"该清而没清掉"的分支环境，逐个重试回收。
//
// 失败不丢：每失败一次 cleanup_attempts +1、last_cleanup_error 记下原因，行仍然留在
// 待清理集合里 —— 下一轮（或服务重启后）继续清，直到成功。
func (s *Service) SweepCleanup(ctx context.Context, limit int) (SweepResult, error) {
	result := SweepResult{}
	if limit <= 0 {
		limit = 20
	}
	if s.provider == nil {
		return result, fmt.Errorf("branchvalidation: no provider configured")
	}
	rows, err := s.pool.Query(ctx, `SELECT id::text, COALESCE(provider_branch_ref,'')
		FROM public.database_branch_validations
		WHERE cleanup_pending AND provider_branch_ref <> ''
		ORDER BY created_at LIMIT $1`, limit)
	if err != nil {
		return result, fmt.Errorf("branchvalidation: sweep query failed: %w", err)
	}
	type pending struct{ id, branch string }
	items := []pending{}
	for rows.Next() {
		item := pending{}
		if err := rows.Scan(&item.id, &item.branch); err != nil {
			rows.Close()
			return result, fmt.Errorf("branchvalidation: sweep scan failed: %w", err)
		}
		items = append(items, item)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return result, err
	}
	result.Scanned = len(items)
	for _, item := range items {
		if err := s.provider.CleanupBranch(ctx, item.branch); err != nil {
			result.Failed++
			if _, execErr := s.pool.Exec(ctx, `UPDATE public.database_branch_validations
				SET cleanup_attempts = cleanup_attempts + 1, last_cleanup_error = $2
				WHERE id = $1`, item.id, err.Error()); execErr != nil {
				return result, fmt.Errorf("branchvalidation: record cleanup failure: %w", execErr)
			}
			continue
		}
		result.Reclaimed++
		if _, err := s.pool.Exec(ctx, `UPDATE public.database_branch_validations
			SET cleanup_pending = false, reclaimed_at = now(), last_cleanup_error = '',
			    cleanup_attempts = cleanup_attempts + 1
			WHERE id = $1`, item.id); err != nil {
			return result, fmt.Errorf("branchvalidation: record reclamation: %w", err)
		}
	}
	return result, nil
}

// ReconcileStale 处理**进程中断**留下的行：卡在 provisioning/validating 且超过
// olderThan 的运行，既不会有结论、也不会有人来清分支。
//
//   - 已经有分支的 → 标成待清理（交给 SweepCleanup 真去删）；
//   - 连分支都没开出来的 → 如实标失败，原因写明"进程中断，未产出结论"。
//
// 这就是"服务重启后未完成任务仍可继续"的落点：重启后第一轮扫描会把它们收干净。
func (s *Service) ReconcileStale(ctx context.Context, olderThan time.Duration) (int, error) {
	if olderThan <= 0 {
		olderThan = 30 * time.Minute
	}
	cutoff := time.Now().Add(-olderThan)
	tag, err := s.pool.Exec(ctx, `UPDATE public.database_branch_validations
		SET cleanup_pending = true,
		    last_cleanup_error = CASE WHEN last_cleanup_error = ''
		        THEN '运行进程中断，环境待回收' ELSE last_cleanup_error END
		WHERE status IN ('provisioning','validating')
		  AND provider_branch_ref <> ''
		  AND created_at < $1`, cutoff)
	if err != nil {
		return 0, fmt.Errorf("branchvalidation: reconcile query failed: %w", err)
	}
	reclaimed := int(tag.RowsAffected())
	tag, err = s.pool.Exec(ctx, `UPDATE public.database_branch_validations
		SET status = 'failed', failure_code = 'ABANDONED_INTERRUPTED',
		    results = '{"reason":"运行进程中断，未产出结论"}'::jsonb
		WHERE status IN ('provisioning','validating')
		  AND COALESCE(provider_branch_ref,'') = ''
		  AND created_at < $1`, cutoff)
	if err != nil {
		return reclaimed, fmt.Errorf("branchvalidation: reconcile abandoned failed: %w", err)
	}
	return reclaimed + int(tag.RowsAffected()), nil
}

// ReclaimIssue 是「交付完成后环境回收」的入口：把这个 issue 关联的分支环境一次性
// 标成待回收，再跑一轮扫描真去删。**证据保留** —— 回收的只是环境，验证结果行、
// 失败原因、迁移逐条结论都原样留着。
//
// 归属链是 task → plan → issue（database_branch_validations.task_id 指向任务，
// 任务的 plan_id 指向计划，计划上挂着 issue_id）。查不到归属的行不动 —— 宁可
// 留给下一轮，也不去删一个说不清归属的环境。
func (s *Service) ReclaimIssue(ctx context.Context, projectID, issueID string) (SweepResult, error) {
	if _, err := s.pool.Exec(ctx, `UPDATE public.database_branch_validations v
		SET cleanup_pending = true
		WHERE v.project_id = $1::uuid
		  AND v.provider_branch_ref <> ''
		  AND v.cleanup_pending = false
		  AND EXISTS (
		    SELECT 1 FROM public.tasks t
		    JOIN public.plans p ON p.id = t.plan_id
		    WHERE t.id = v.task_id AND p.issue_id = $2)`,
		projectID, issueID); err != nil {
		return SweepResult{}, fmt.Errorf("branchvalidation: reclaim mark failed: %w", err)
	}
	return s.SweepCleanup(ctx, 50)
}
