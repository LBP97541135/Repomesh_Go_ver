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

// Purge hard-deletes an already-archived issue: snapshots (issue content
// scope, conversation, changesets, events), decision chain nodes and audit
// events. 409-shaped error when the issue is not archived.
func (m *Maintenance) Purge(ctx context.Context, issueID string) (PurgeResult, error) {
	tx, err := m.pool.Begin(ctx)
	if err != nil {
		return PurgeResult{}, err
	}
	defer tx.Rollback(ctx)
	// 0033: the immutable-fact triggers allow deletes only under this
	// transaction-scoped flag; every other write path still rejects them.
	if _, err = tx.Exec(ctx, `SET LOCAL repomesh.purge_mode='on'`); err != nil {
		return PurgeResult{}, err
	}
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

	if _, err = tx.Exec(ctx, "DELETE FROM repomesh_issues.issue_content_scope WHERE issue_id=$1", issueID); err != nil {
		return PurgeResult{}, err
	}
	if _, err = tx.Exec(ctx, "DELETE FROM repomesh_issues.issue_repository_scope WHERE issue_id=$1", issueID); err != nil {
		return PurgeResult{}, err
	}
	if _, err = tx.Exec(ctx, "DELETE FROM repomesh_issues.conversation_content_scope WHERE conversation_id IN (SELECT main_conversation_id FROM repomesh_issues.issues WHERE id=$1)", issueID); err != nil {
		return PurgeResult{}, err
	}
	if _, err = tx.Exec(ctx, "DELETE FROM repomesh_issues.conversation_cards WHERE conversation_id IN (SELECT main_conversation_id FROM repomesh_issues.issues WHERE id=$1)", issueID); err != nil {
		return PurgeResult{}, err
	}
	if _, err = tx.Exec(ctx, "DELETE FROM repomesh_issues.conversations WHERE id IN (SELECT main_conversation_id FROM repomesh_issues.issues WHERE id=$1)", issueID); err != nil {
		return PurgeResult{}, err
	}
	if _, err = tx.Exec(ctx, "DELETE FROM repomesh_issues.issue_events WHERE issue_id=$1", issueID); err != nil {
		return PurgeResult{}, err
	}
	if _, err = tx.Exec(ctx, "DELETE FROM repomesh_issues.changesets WHERE issue_id=$1", issueID); err != nil {
		return PurgeResult{}, err
	}
	if _, err = tx.Exec(ctx, "DELETE FROM public.decision_chain_nodes WHERE requirement_key LIKE $1 || '%'", issueID); err != nil {
		return PurgeResult{}, err
	}
	// The old predicate `event_type='IssuePurged' IS NOT NULL` bound as
	// `event_type = ('IssuePurged' IS NOT NULL)` — text = boolean, a type
	// error. Purge deletes every audit event of this aggregate, then writes
	// exactly one fresh IssuePurged record below.
	if _, err = tx.Exec(ctx, "DELETE FROM public.events WHERE aggregate_id=$1::uuid", issueID); err != nil {
		return PurgeResult{}, err
	}
	auditJSON, _ := json.Marshal(map[string]any{"issue_id": issueID, "project_id": projectID, "purged_at": time.Now().UTC()})
	// events.correlation_id is NOT NULL: give the purge record its own
	// correlation id and fill the channel/aggregate_type identity columns.
	if _, err = tx.Exec(ctx,
		"INSERT INTO public.events (id, channel, event_type, correlation_id, aggregate_type, aggregate_id, payload, recorded_at)"+
			" VALUES (gen_random_uuid(), 'issue', 'IssuePurged', gen_random_uuid(), 'issue', $1::uuid, $2::jsonb, now())",
		issueID, string(auditJSON)); err != nil {
		return PurgeResult{}, err
	}
	if _, err = tx.Exec(ctx, "DELETE FROM repomesh_issues.issue_discoveries WHERE issue_id=$1", issueID); err != nil {
		return PurgeResult{}, err
	}
	if _, err = tx.Exec(ctx, "DELETE FROM repomesh_issues.creation_operations WHERE id IN (SELECT creation_operation_id FROM repomesh_issues.issues WHERE id=$1)", issueID); err != nil {
		return PurgeResult{}, err
	}
	if _, err = tx.Exec(ctx, "DELETE FROM repomesh_issues.issues WHERE id=$1", issueID); err != nil {
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
