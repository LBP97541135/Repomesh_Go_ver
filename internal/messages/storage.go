package messages

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"repomesh.local/repomesh/internal/access"
)

func (s *MessageService) begin(ctx context.Context) (pgx.Tx, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return nil, unavailable()
	}
	if _, err := tx.Exec(ctx, `SET LOCAL lock_timeout='2s'`); err != nil {
		rollbackTx(tx)
		return nil, unavailable()
	}
	if _, err := tx.Exec(ctx, `SET LOCAL statement_timeout='10s'`); err != nil {
		rollbackTx(tx)
		return nil, unavailable()
	}
	if _, err := tx.Exec(ctx, `SET LOCAL synchronous_commit=on`); err != nil {
		rollbackTx(tx)
		return nil, unavailable()
	}
	return tx, nil
}

func rollbackTx(tx pgx.Tx) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = tx.Rollback(ctx)
}

type submissionRecord struct {
	canonical []byte
	receipt   []byte
	removed   bool
}

func readSubmission(ctx context.Context, tx pgx.Tx, command SubmissionCommand) (submissionRecord, error) {
	var record submissionRecord
	var canonical, exact, digestBytes, receipt []byte
	var messageID *string
	var removed *time.Time
	err := tx.QueryRow(ctx, `SELECT canonical_input, exact_input, input_digest, receipt, message_id, removed_at
		FROM repomesh_messages.message_submissions
		WHERE project_id=$1 AND conversation_id=$2 AND actor=$3 AND entry='conversation_message' AND submission_id=$4`,
		command.ProjectID, command.ConversationID, command.Actor, command.SubmissionID).Scan(
		&canonical, &exact, &digestBytes, &receipt, &messageID, &removed)
	if errors.Is(err, pgx.ErrNoRows) {
		return submissionRecord{}, nil
	}
	if err != nil {
		return submissionRecord{}, unavailable()
	}
	record.canonical = canonical
	record.receipt = receipt
	record.removed = removed != nil
	return record, nil
}

// commitSubmit performs the aggregate write: conversation lock, sequence
// allocation, message row, interpretation entry, logical request registration,
// and the completed submission receipt.
func (s *MessageService) commitSubmit(ctx context.Context, principal access.ProjectPrincipal, command SubmissionCommand, input submitInput, canonical []byte) (SubmissionReceipt, error) {
	tx, err := s.begin(ctx)
	if err != nil {
		return SubmissionReceipt{}, err
	}
	defer rollbackTx(tx)
	if err := s.authorization.LockProjectPrincipal(ctx, tx, principal); err != nil {
		return SubmissionReceipt{}, err
	}
	// Conversation must exist in this project and stay live.
	var live bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM repomesh_issues.conversations
		WHERE project_id=$1 AND id=$2 AND removed_at IS NULL)`, command.ProjectID, command.ConversationID).Scan(&live); err != nil {
		return SubmissionReceipt{}, unavailable()
	}
	if !live {
		return SubmissionReceipt{}, failure(404, "RESOURCE_NOT_FOUND")
	}
	// Sequence is allocated under the conversation row lock so all writers
	// share one order (design §7 pagination rule).
	var sequence int64
	if err := tx.QueryRow(ctx, `SELECT COALESCE(MAX(sequence),0)+1 FROM repomesh_messages.conversation_messages
		WHERE project_id=$1 AND conversation_id=$2`, command.ProjectID, command.ConversationID).Scan(&sequence); err != nil {
		return SubmissionReceipt{}, unavailable()
	}
	messageID, err := newID("msg_")
	if err != nil {
		return SubmissionReceipt{}, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO repomesh_messages.conversation_messages
		(id, project_id, conversation_id, sequence, author_kind, actor_id, body, reply_to_clarification)
		VALUES ($1,$2,$3,$4,'user',$5,$6,NULLIF($7,''))`,
		messageID, command.ProjectID, command.ConversationID, sequence, principal.ActorID(), input.body, input.replyTo); err != nil {
		return SubmissionReceipt{}, unavailable()
	}
	if _, err := tx.Exec(ctx, `INSERT INTO repomesh_messages.message_processing_entries
		(message_id, project_id, conversation_id, kind) VALUES ($1,$2,$3,'interpret_message')`,
		messageID, command.ProjectID, command.ConversationID); err != nil {
		return SubmissionReceipt{}, unavailable()
	}
	requestID, err := newID("lwr_")
	if err != nil {
		return SubmissionReceipt{}, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO repomesh_messages.logical_work_requests
		(id, project_id, conversation_id, source_message_id, source_revision, actor, source_unit, state, revision)
		VALUES ($1,$2,$3,$4,1,$5,'whole_message_single_request','pending',1)`,
		requestID, command.ProjectID, command.ConversationID, messageID, principal.ActorID()); err != nil {
		return SubmissionReceipt{}, unavailable()
	}
	receipt := SubmissionReceipt{
		SubmissionID:   command.SubmissionID,
		MessageID:      messageID,
		ConversationID: command.ConversationID,
		Sequence:       sequence,
		SubmittedAt:    time.Now().UTC().Format(time.RFC3339Nano),
		ReplyTo:        input.replyTo,
	}
	receiptJSON, err := json.Marshal(receipt)
	if err != nil {
		return SubmissionReceipt{}, unavailable()
	}
	commandTag, err := tx.Exec(ctx, `UPDATE repomesh_messages.message_submissions
		SET message_id=$5, receipt=$6::jsonb
		WHERE project_id=$1 AND conversation_id=$2 AND actor=$3 AND entry='conversation_message' AND submission_id=$4 AND receipt IS NULL`,
		command.ProjectID, command.ConversationID, command.Actor, command.SubmissionID, messageID, receiptJSON)
	if err != nil {
		return SubmissionReceipt{}, unavailable()
	}
	if commandTag.RowsAffected() != 1 {
		return SubmissionReceipt{}, unavailable()
	}
	if err := tx.Commit(ctx); err != nil {
		return SubmissionReceipt{}, unavailable()
	}
	return receipt, nil
}
