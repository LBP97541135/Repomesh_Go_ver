package messages

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"repomesh.local/repomesh/internal/access"
)

// ListMessages returns the conversation message page (committed sequence
// ascending). The cursor is the last sequence served; it bounds the page but
// is not an authorization artifact.
func (s *MessageService) ListMessages(ctx context.Context, principal access.ProjectPrincipal, projectID, conversationID string, limit int, cursor string) ([]byte, error) {
	if limit < 1 || limit > 100 {
		return nil, failure(422, "VALIDATION_FAILED")
	}
	tx, err := s.begin(ctx)
	if err != nil {
		return nil, err
	}
	defer rollbackTx(tx)
	if err := s.authorization.CheckProjectPrincipal(ctx, tx, principal); err != nil {
		return nil, err
	}
	var live bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM repomesh_issues.conversations
		WHERE project_id=$1 AND id=$2 AND removed_at IS NULL)`, projectID, conversationID).Scan(&live); err != nil {
		return nil, unavailable()
	}
	if !live {
		return nil, failure(404, "RESOURCE_NOT_FOUND")
	}
	after := int64(0)
	if cursor != "" {
		if _, err := fmt.Sscanf(cursor, "%d", &after); err != nil {
			return nil, failure(400, "INVALID_CURSOR")
		}
	}
	rows, err := tx.Query(ctx, `SELECT id, sequence, author_kind, actor_id, body, created_at
		FROM repomesh_messages.conversation_messages
		WHERE project_id=$1 AND conversation_id=$2 AND removed_at IS NULL AND sequence>$3
		ORDER BY sequence LIMIT $4`, projectID, conversationID, after, limit)
	if err != nil {
		return nil, unavailable()
	}
	defer rows.Close()
	page := struct {
		Items      []messageRow `json:"items"`
		NextCursor *string      `json:"nextCursor"`
	}{Items: []messageRow{}}
	for rows.Next() {
		var row messageRow
		var createdAt time.Time
		if rows.Scan(&row.ID, &row.Sequence, &row.AuthorKind, &row.ActorID, &row.Body, &createdAt) != nil {
			return nil, unavailable()
		}
		row.CreatedAt = createdAt.UTC().Format(time.RFC3339Nano)
		page.Items = append(page.Items, row)
	}
	if rows.Err() != nil {
		return nil, unavailable()
	}
	if len(page.Items) == limit && len(page.Items) > 0 {
		last := page.Items[len(page.Items)-1].Sequence
		text := fmt.Sprintf("%d", last)
		page.NextCursor = &text
	}
	encoded, err := json.Marshal(page)
	if err != nil {
		return nil, unavailable()
	}
	return encoded, nil
}

type messageRow struct {
	ID         string `json:"id"`
	Sequence   int64  `json:"sequence"`
	AuthorKind string `json:"authorKind"`
	ActorID    string `json:"actorId"`
	Body       string `json:"body"`
	CreatedAt  string `json:"createdAt"`
}

// GetSubmission returns the frozen submission receipt for one operation key.
func (s *MessageService) GetSubmission(ctx context.Context, principal access.ProjectPrincipal, projectID, submissionID string) (SubmissionReceipt, error) {
	tx, err := s.begin(ctx)
	if err != nil {
		return SubmissionReceipt{}, err
	}
	defer rollbackTx(tx)
	if err := s.authorization.CheckProjectPrincipal(ctx, tx, principal); err != nil {
		return SubmissionReceipt{}, err
	}
	var receipt []byte
	var removed *time.Time
	err = tx.QueryRow(ctx, `SELECT receipt, removed_at FROM repomesh_messages.message_submissions
		WHERE project_id=$1 AND actor=$2 AND entry='conversation_message' AND submission_id=$3`,
		projectID, principal.ActorID(), submissionID).Scan(&receipt, &removed)
	if errors.Is(err, pgx.ErrNoRows) {
		return SubmissionReceipt{}, failure(404, "MESSAGE_SUBMISSION_NOT_FOUND")
	}
	if err != nil {
		return SubmissionReceipt{}, unavailable()
	}
	if removed != nil {
		return SubmissionReceipt{}, failure(410, "MESSAGE_SUBMISSION_RESULT_REMOVED")
	}
	if receipt == nil {
		return SubmissionReceipt{}, failure(404, "MESSAGE_SUBMISSION_NOT_FOUND")
	}
	var decoded SubmissionReceipt
	if err := json.Unmarshal(receipt, &decoded); err != nil {
		return SubmissionReceipt{}, unavailable()
	}
	return decoded, nil
}
