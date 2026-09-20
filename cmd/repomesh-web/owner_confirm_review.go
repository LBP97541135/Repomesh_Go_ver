package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"

	"repomesh.local/repomesh/internal/responsibility"
	"repomesh.local/repomesh/internal/scm"
)

// ownerConfirmReviewRecorder 把「仓库 Owner 确认」桥到交付闸门的 review 事件。
//
// 2026-09-21 用户裁定：**仓库 Owner 确认 = 交付闸门四项里的 review**。此前 review
// 只有"经理审批通过"一个来源，线上实测 19 个有 PR 的变更集里只有 9 个有 review，
// 其余 10 个闸门永远关着 —— 用户在交付列车上点合并必然失败。
//
// ⚠️ 两套仓库 id 口径必须在这里对齐，否则会**静默匹配 0 条**（看起来接了线，
// 实际一条都不记）：
//   - Owner 确认传进来的是**项目仓 id**（`repo_…`，即 repomesh_projects.repositories.id）
//   - `tasks.repository_id` 存的是**全名**（`owner/name`）
//
// 所以先解析全名；解析不出来就**如实返回错误**（调用方打日志），不猜、不 LIKE。
func ownerConfirmReviewRecorder(pool *pgxpool.Pool, scmSvc *scm.Service) responsibility.ReviewRecorder {
	return func(ctx context.Context, planID, repositoryID, actor string) error {
		if pool == nil || scmSvc == nil || planID == "" || repositoryID == "" {
			return nil
		}
		var fullName string
		if err := pool.QueryRow(ctx,
			`SELECT owner || '/' || name FROM repomesh_projects.repositories WHERE id=$1`,
			repositoryID).Scan(&fullName); err != nil {
			return fmt.Errorf("owner-confirm: 仓库 id %s 解析不出全名：%w", repositoryID, err)
		}
		rows, err := pool.Query(ctx, `SELECT cs.id::text
			FROM public.change_sets cs
			JOIN public.tasks t ON t.id = cs.task_id
			WHERE t.plan_id = $1::uuid AND t.repository_id = $2`, planID, fullName)
		if err != nil {
			return fmt.Errorf("owner-confirm: 查该仓的变更集失败：%w", err)
		}
		var ids []string
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return fmt.Errorf("owner-confirm: 读变更集失败：%w", err)
			}
			ids = append(ids, id)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return fmt.Errorf("owner-confirm: 变更集遍历失败：%w", err)
		}
		// 这条计划还没冻出变更集（任务还没到交付段）不是错误：Owner 确认照样成立，
		// 只是此刻还没有闸门可记。如实返回 nil，不编一条记录。
		if len(ids) == 0 {
			return nil
		}
		payload, _ := json.Marshal(map[string]string{
			"actor": actor, "source": "owner_confirm", "repository": fullName,
		})
		var failed int
		for _, id := range ids {
			if err := scmSvc.RecordEvent(ctx, id, "review", string(payload)); err != nil {
				failed++
				slog.Warn("owner-confirm: 补记交付闸门 review 失败",
					"changeSet", id, "repo", fullName, "err", err)
			}
		}
		if failed == len(ids) {
			return fmt.Errorf("owner-confirm: %d 个变更集全部补记失败", failed)
		}
		slog.Info("owner-confirm: 已补记交付闸门 review",
			"repo", fullName, "changeSets", len(ids), "failed", failed, "actor", actor)
		return nil
	}
}
