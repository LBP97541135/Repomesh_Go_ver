package issues

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"repomesh.local/repomesh/internal/access"
)

// Maintenance exposes the content-removal maintenance surface. It is wired in
// the composition root and never reached from the web layer.
type Maintenance struct {
	service *Service
}

// NewMaintenance returns the maintenance facade.
func NewMaintenance(service *Service) *Maintenance { return &Maintenance{service: service} }

// RemoveIssueBody performs the body-removal sweep for one issue: redact the
// issue title and description, tombstone the creation operation, redact the
// derived conversation title, cancel the unsent continuation, and never delete
// or resurrect anything. Rows with external facts already recorded abort the
// sweep.
func (m *Maintenance) RemoveIssueBody(ctx context.Context, principal access.ProjectPrincipal, projectID, issueID string) error {
	tx, err := m.service.beginCreate(ctx)
	if err != nil {
		return err
	}
	defer rollbackTx(tx)
	if err := m.service.authorization.LockProjectPrincipal(ctx, tx, principal); err != nil {
		return err
	}
	var operationID string
	var mainConversationID string
	var removed *time.Time
	err = tx.QueryRow(ctx, `SELECT creation_operation_id, main_conversation_id, removed_at
		FROM repomesh_issues.issues WHERE project_id=$1 AND id=$2 FOR UPDATE`,
		projectID, issueID).Scan(&operationID, &mainConversationID, &removed)
	if errors.Is(err, pgx.ErrNoRows) {
		return failure(404, "RESOURCE_NOT_FOUND")
	}
	if err != nil {
		return unavailable()
	}
	if removed != nil {
		return nil
	}

	// External facts (a sent continuation or provider-side state) abort the sweep.
	var blocked int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM repomesh_issues.continuation_work
		WHERE project_id=$1 AND issue_id=$2 AND state='blocked'`, projectID, issueID).Scan(&blocked); err != nil {
		return unavailable()
	}
	if blocked == 0 {
		return failure(409, "CONTENT_ALREADY_CONSUMED")
	}
	now := time.Now().UTC()
	// Redact the derived conversation title (the sanctioned body cleanup);
	// immutable_issue_facts keeps the issue row itself frozen, so removal is
	// recorded on the operation and continuation instead.
	if _, err := tx.Exec(ctx, `UPDATE repomesh_issues.conversations
		SET title=NULL, title_redacted_at=$3 WHERE project_id=$1 AND id=$2 AND title IS NOT NULL`,
		projectID, mainConversationID, now); err != nil {
		return unavailable()
	}

	// Cancel the unsent continuation; the cancelled-state CHECK pins all three
	// facts together (state, reason, cancelled_at), so the reason must move in
	// the same statement or the sweep fails with a check violation.
	if _, err := tx.Exec(ctx, `UPDATE repomesh_issues.continuation_work
		SET state='cancelled', reason='CONTENT_REMOVED', cancelled_at=$3 WHERE project_id=$1 AND issue_id=$2 AND state='blocked'`,
		projectID, issueID, now); err != nil {
		return unavailable()
	}

	// Tombstone the creation operation: removed_at set, canonical/exact/receipt
	// dropped (cleanup CHECK). immutable_creation_identity restricts removed_at
	// to the NULL -> timestamp transition only.
	if _, err := tx.Exec(ctx, `UPDATE repomesh_issues.creation_operations
		SET removed_at=$5, canonical_input=NULL, exact_input=NULL, input_digest=NULL, receipt=NULL
		WHERE project_id=$1 AND actor=$2 AND entry='issue_page' AND creation_id=$3 AND id=$4 AND removed_at IS NULL`,
		projectID, m.actorFor(principal), operationID, operationID, now); err != nil {
		return unavailable()
	}
	if err := tx.Commit(ctx); err != nil {
		return unavailable()
	}
	return nil
}

func (m *Maintenance) actorFor(principal access.ProjectPrincipal) string {
	return principal.ActorID()
}
