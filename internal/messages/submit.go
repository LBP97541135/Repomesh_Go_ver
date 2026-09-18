package messages

import (
	"context"

	"repomesh.local/repomesh/internal/access"
)

// submitInput is the parsed message body.
type submitInput struct {
	body    string
	replyTo string
}

// SubmissionReceipt freezes the committed message facts; current question
// state is deliberately absent (retrieved via the separate read).
type SubmissionReceipt struct {
	SubmissionID   string `json:"submissionId"`
	MessageID      string `json:"messageId"`
	ConversationID string `json:"conversationId"`
	Sequence       int64  `json:"sequence"`
	SubmittedAt    string `json:"submittedAt"`
	ReplyTo        string `json:"replyTo,omitempty"`
}

// Submit runs the message submission transaction (design §5.1): idempotent
// replay, out-of-transaction scope check, then a short transaction that
// allocates the sequence under the conversation row and records the
// interpretation entries.
func (s *MessageService) Submit(ctx context.Context, principal access.ProjectPrincipal, command SubmissionCommand) (SubmissionReceipt, bool, error) {
	input, err := parseSubmitInput(command.Body)
	if err != nil {
		return SubmissionReceipt{}, false, err
	}
	canonical := canonicalizeSubmit(input, command)

	tx, err := s.begin(ctx)
	if err != nil {
		return SubmissionReceipt{}, false, err
	}
	defer rollbackTx(tx)
	if err := s.authorization.LockProjectPrincipal(ctx, tx, principal); err != nil {
		return SubmissionReceipt{}, false, err
	}
	existing, err := readSubmission(ctx, tx, command)
	if err != nil {
		return SubmissionReceipt{}, false, err
	}
	if existing.removed {
		return SubmissionReceipt{}, false, failure(410, "MESSAGE_SUBMISSION_RESULT_REMOVED")
	}
	if existing.receipt != nil {
		replay, decodeErr := decodeSubmissionReceipt(existing.receipt)
		if decodeErr != nil {
			return SubmissionReceipt{}, false, unavailable()
		}
		return replay, true, nil
	}
	if existing.canonical != nil {
		return SubmissionReceipt{}, false, failure(409, "IDEMPOTENCY_CONFLICT")
	}
	// New submission: reserve the idempotency slot before the aggregate write.
	if _, err := tx.Exec(ctx, `INSERT INTO repomesh_messages.message_submissions
		(project_id, conversation_id, actor, entry, submission_id, id, canonical_input, exact_input, input_digest)
		VALUES ($1,$2,$3,'conversation_message',$4,$4,$5,$6,$7) ON CONFLICT DO NOTHING`,
		command.ProjectID, command.ConversationID, principal.ActorID(), command.SubmissionID,
		canonical, command.Body, digest(command.Body)); err != nil {
		return SubmissionReceipt{}, false, unavailable()
	}
	// 幂等槽必须先持久化:提交本事务,聚合写在下一个短事务里完成并回填
	// receipt。之前这里是 rollbackTx——槽跟着回滚,聚合写的 receipt UPDATE
	// 永远匹配 0 行,所有首提一律 503 RESULT_UNCONFIRMED(读面正常所以一直没暴露)。
	if err := tx.Commit(ctx); err != nil {
		return SubmissionReceipt{}, false, unavailable()
	}

	message, err := s.commitSubmit(ctx, principal, command, input, canonical)
	if err != nil {
		return SubmissionReceipt{}, false, err
	}
	return message, false, nil
}
