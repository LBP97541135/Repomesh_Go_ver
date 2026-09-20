package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Maintenance implements issue archive (v0.5 tombstone) and purge (hard
// delete of snapshots, decision chain and audit events, keeping one
// IssuePurged record).
type Maintenance struct {
	pool *pgxpool.Pool
}

// NewMaintenance builds the maintenance facade.
func NewMaintenance(pool *pgxpool.Pool) *Maintenance { return &Maintenance{pool: pool} }

// Archive marks one issue archived; repeated calls are idempotent and
// return the original archived_at.
func (m *Maintenance) Archive(ctx context.Context, issueID string) (string, error) {
	var archivedAt *time.Time
	err := m.pool.QueryRow(ctx,
		"UPDATE repomesh_issues.issues SET archived_at=COALESCE(archived_at, now())"+
			" WHERE id=$1 RETURNING archived_at", issueID).Scan(&archivedAt)
	if err != nil {
		return "", fmt.Errorf("issues: archive: %w", err)
	}
	return archivedAt.Format(time.RFC3339Nano), nil
}

// PurgeResult is the purge receipt counters.
type PurgeResult struct {
	Snapshots          int64 `json:"snapshots"`
	DecisionChainNodes int64 `json:"decision_chain_nodes"`
	AuditEvents        int64 `json:"audit_events"`
}

// Purge soft-deletes an already-archived issue（2026-09-20 用户裁定：不真删数据）。
//
// 历史包袱先说清：这里此前是**硬删除**——一条事务删 12 张表（issue_content_scope、
// issue_repository_scope、conversation_content_scope、conversation_cards、
// conversations、issue_events、changesets、decision_chain_nodes、events、
// issue_discoveries、creation_operations、issues），只留 issue_purge_log 与一条
// IssuePurged 审计。但 schema 早就为软删除留好了位：issues.removed_at（0005），
// 所有读面（列表/创建条件/消费/卡点）都已经在过滤 `removed_at IS NULL`，0018 触发器
// 还规定 removed_at 一旦设置即终态——当初设计意图就是墓碑，只是没人写它。
//
// 现在 Purge = 打 removed_at 墓碑 + 照旧写回执与审计。回执里的三个计数从"删掉了
// 多少"变成"随墓碑一起隐藏了多少"，字段名不变（前端契约不动）。
//
// **可恢复性如实说**：0018 触发器禁止把 removed_at 改回去（final once set），所以
// 这是一次性删除；要支持恢复，得先放宽那条触发器——要做说一声。
func (m *Maintenance) Purge(ctx context.Context, issueID string) (PurgeResult, error) {
	tx, err := m.pool.Begin(ctx)
	if err != nil {
		return PurgeResult{}, err
	}
	defer tx.Rollback(ctx)
	var projectID string
	var archivedAt *time.Time
	err = tx.QueryRow(ctx,
		"SELECT project_id, archived_at FROM repomesh_issues.issues WHERE id=$1 FOR UPDATE", issueID).
		Scan(&projectID, &archivedAt)
	if err != nil {
		return PurgeResult{}, err
	}
	if archivedAt == nil {
		return PurgeResult{}, fmt.Errorf("issues: purge requires the issue to be archived first")
	}
	var linked bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM public.plans WHERE issue_id=$1)
		OR EXISTS(SELECT 1 FROM public.task_repository_scopes WHERE issue_id=$1)`, issueID).Scan(&linked); err != nil {
		return PurgeResult{}, err
	}
	if linked {
		return PurgeResult{}, fmt.Errorf("%w: Issue 已关联执行计划或任务，仅支持归档，不能清除执行范围", ErrConflict)
	}
	result := PurgeResult{}
	_ = tx.QueryRow(ctx, "SELECT count(*) FROM repomesh_issues.issue_content_scope WHERE issue_id=$1", issueID).Scan(&result.Snapshots)
	_ = tx.QueryRow(ctx, "SELECT count(*) FROM public.decision_chain_nodes WHERE requirement_key LIKE $1 || '%'", issueID).Scan(&result.DecisionChainNodes)
	_ = tx.QueryRow(ctx, "SELECT count(*) FROM public.events WHERE aggregate_id::text=$1", issueID).Scan(&result.AuditEvents)

	if _, err = tx.Exec(ctx,
		"UPDATE repomesh_issues.issues SET removed_at=now()"+
			" WHERE id=$1 AND removed_at IS NULL", issueID); err != nil {
		return PurgeResult{}, err
	}
	auditJSON, _ := json.Marshal(map[string]any{"issue_id": issueID, "project_id": projectID, "purged_at": time.Now().UTC(), "mode": "soft"})
	if _, err = tx.Exec(ctx,
		"INSERT INTO public.events (id, channel, event_type, correlation_id, aggregate_type, aggregate_id, payload, recorded_at)"+
			" VALUES (gen_random_uuid(), 'issue', 'IssuePurged', gen_random_uuid(), 'issue', $1::uuid, $2::jsonb, now())",
		issueID, string(auditJSON)); err != nil {
		return PurgeResult{}, err
	}
	if _, err = tx.Exec(ctx,
		"INSERT INTO repomesh_issues.issue_purge_log (issue_id, project_id, snapshots, decision_chain_nodes, audit_events, purged_by, purged_at)"+
			" VALUES ($1, $2, $3, $4, $5, 'purge', now())"+
			" ON CONFLICT (issue_id) DO UPDATE SET snapshots=EXCLUDED.snapshots, decision_chain_nodes=EXCLUDED.decision_chain_nodes, audit_events=EXCLUDED.audit_events, purged_at=now()",
		issueID, projectID, result.Snapshots, result.DecisionChainNodes, result.AuditEvents); err != nil {
		return PurgeResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return PurgeResult{}, err
	}
	return result, nil
}
