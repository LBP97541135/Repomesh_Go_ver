package messages

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"repomesh.local/repomesh/internal/access"
)

// ClarificationView is the read projection of one clarification.
type ClarificationView struct {
	ID                string                `json:"id"`
	RequestID         string                `json:"requestId"`
	State             string                `json:"state"`
	Revision          int64                 `json:"revision"`
	QuestionMessageID string                `json:"questionMessageId"`
	AnswerMessageID   *string               `json:"answerMessageId"`
	Resolution        *TargetResolutionView `json:"resolution"`
}

// TargetResolutionView freezes the final interpretation.
type TargetResolutionView struct {
	ResolutionID string  `json:"resolutionId"`
	InputKind    string  `json:"inputKind"`
	Outcome      string  `json:"outcome"`
	IssueID      *string `json:"issueId"`
}

// DecideTarget runs the controlled decide_message_target action (design §6):
// exactly one of root_message (request pending, no question) or
// clarification_answer (question answer_saved) may resolve a request. The
// outcome is issue_target with a same-project issue or no_work with none.
func (s *MessageService) DecideTarget(ctx context.Context, principal access.ProjectPrincipal, command DecideCommand) (TargetResolutionView, error) {
	if err := command.validate(); err != nil {
		return TargetResolutionView{}, err
	}
	tx, err := s.begin(ctx)
	if err != nil {
		return TargetResolutionView{}, err
	}
	defer rollbackTx(tx)
	if err := s.authorization.LockProjectPrincipal(ctx, tx, principal); err != nil {
		return TargetResolutionView{}, err
	}
	request, err := readWorkRequest(ctx, tx, command.RequestID, command.ProjectID)
	if err != nil {
		return TargetResolutionView{}, err
	}
	// Branch eligibility is a server-side fact, not a caller-chosen parameter.
	inputKind := ""
	if command.AnswerMessageID != "" {
		inputKind = "clarification_answer"
	} else {
		inputKind = "root_message"
	}
	switch inputKind {
	case "root_message":
		if request.state != "pending" || request.currentClarificationID != "" {
			return TargetResolutionView{}, failure(409, "STALE_PROCESSING_CONTEXT")
		}
	case "clarification_answer":
		if request.state != "answer_pending" || request.currentClarificationID == "" {
			return TargetResolutionView{}, failure(409, "STALE_PROCESSING_CONTEXT")
		}
		var answerMessage string
		if err := tx.QueryRow(ctx, `SELECT answer_message_id FROM repomesh_messages.clarification_answers
			WHERE clarification_id=$1`, request.currentClarificationID).Scan(&answerMessage); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return TargetResolutionView{}, failure(409, "STALE_PROCESSING_CONTEXT")
			}
			return TargetResolutionView{}, unavailable()
		}
		if answerMessage != command.AnswerMessageID {
			return TargetResolutionView{}, failure(409, "STALE_PROCESSING_CONTEXT")
		}
	}
	if command.Outcome == "issue_target" {
		var owned bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM repomesh_issues.issues
			WHERE project_id=$1 AND id=$2 AND removed_at IS NULL)`, command.ProjectID, *command.IssueID).Scan(&owned); err != nil {
			return TargetResolutionView{}, unavailable()
		}
		if !owned {
			return TargetResolutionView{}, failure(404, "RESOURCE_NOT_FOUND")
		}
	}
	resolutionID, err := newID("res_")
	if err != nil {
		return TargetResolutionView{}, err
	}
	evidence, err := json.Marshal(command.Evidence)
	if err != nil {
		evidence = []byte("[]")
	}
	if _, err := tx.Exec(ctx, `INSERT INTO repomesh_messages.target_resolutions
		(logical_request_id, resolution_id, input_kind, outcome, issue_id, evidence)
		VALUES ($1,$2,$3,$4,NULLIF($5,''),$6::jsonb)`,
		command.RequestID, resolutionID, inputKind, command.Outcome, derefIssue(command.IssueID), evidence); err != nil {
		return TargetResolutionView{}, unavailable()
	}
	if _, err := tx.Exec(ctx, `UPDATE repomesh_messages.logical_work_requests
		SET state='resolved', revision=revision+1, resolution_id=$2 WHERE id=$1`, command.RequestID, resolutionID); err != nil {
		return TargetResolutionView{}, unavailable()
	}
	if _, err := tx.Exec(ctx, `INSERT INTO repomesh_messages.control_operations
		(command_slot_id, project_id, action, schema_version, canonical_input, result)
		VALUES ($1,$2,'decide_message_target',1,$3::jsonb,$4::jsonb)`,
		command.CommandSlotID, command.ProjectID, command.canonicalJSON(), controlResult(resolutionID, inputKind, command.Outcome, command.IssueID)); err != nil {
		return TargetResolutionView{}, unavailable()
	}
	if err := tx.Commit(ctx); err != nil {
		return TargetResolutionView{}, unavailable()
	}
	view := TargetResolutionView{ResolutionID: resolutionID, InputKind: inputKind, Outcome: command.Outcome, IssueID: command.IssueID}
	return view, nil
}

// DecideCommand is one controlled decide_message_target invocation bound by
// the trusted adapter: actor/project/request come from server-side state, not
// from the model.
type DecideCommand struct {
	RequestID       string
	ProjectID       string
	Outcome         string
	IssueID         *string
	AnswerMessageID string
	CommandSlotID   string
	Evidence        []EvidenceRef
}

// EvidenceRef is one immutable quote: a message id plus a half-open rune
// interval within that message's body.
type EvidenceRef struct {
	MessageID string `json:"messageId"`
	Start     int    `json:"start"`
	End       int    `json:"end"`
}

func (c DecideCommand) validate() error {
	if c.RequestID == "" || c.ProjectID == "" || c.CommandSlotID == "" {
		return failure(422, "VALIDATION_FAILED")
	}
	switch c.Outcome {
	case "issue_target":
		if c.IssueID == nil || *c.IssueID == "" {
			return fieldFailure("issueId", "REQUIRED")
		}
	case "no_work":
		if c.IssueID != nil {
			return fieldFailure("issueId", "FORBIDDEN")
		}
	default:
		return fieldFailure("outcome", "INVALID")
	}
	if len(c.Evidence) > 20 {
		return fieldFailure("evidence", "TOO_MANY")
	}
	for _, ref := range c.Evidence {
		if ref.MessageID == "" || ref.Start < 0 || ref.Start >= ref.End {
			return fieldFailure("evidence", "INVALID_INTERVAL")
		}
	}
	return nil
}

func (c DecideCommand) canonicalJSON() []byte {
	document := map[string]any{"outcome": c.Outcome, "evidence": c.Evidence}
	if c.IssueID != nil {
		document["issueId"] = *c.IssueID
	}
	encoded, err := json.Marshal(document)
	if err != nil {
		return []byte("{}")
	}
	return encoded
}

func controlResult(resolutionID, inputKind, outcome string, issueID *string) []byte {
	result := map[string]any{"resolutionId": resolutionID, "inputKind": inputKind, "outcome": outcome}
	if issueID != nil {
		result["issueId"] = *issueID
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return []byte("{}")
	}
	return encoded
}

func derefIssue(issueID *string) string {
	if issueID == nil {
		return ""
	}
	return *issueID
}

type workRequestRow struct {
	id                     string
	state                  string
	currentClarificationID string
}

func readWorkRequest(ctx context.Context, tx pgx.Tx, id, projectID string) (workRequestRow, error) {
	var row workRequestRow
	err := tx.QueryRow(ctx, `SELECT id, state, COALESCE(current_clarification_id,'')
		FROM repomesh_messages.logical_work_requests WHERE id=$1 AND project_id=$2`, id, projectID).
		Scan(&row.id, &row.state, &row.currentClarificationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return workRequestRow{}, failure(404, "RESOURCE_NOT_FOUND")
	}
	if err != nil {
		return workRequestRow{}, unavailable()
	}
	return row, nil
}

// GetClarification returns the current read projection for one request.
func (s *MessageService) GetClarification(ctx context.Context, principal access.ProjectPrincipal, projectID, requestID string) (ClarificationView, error) {
	tx, err := s.begin(ctx)
	if err != nil {
		return ClarificationView{}, err
	}
	defer rollbackTx(tx)
	if err := s.authorization.LockProjectPrincipal(ctx, tx, principal); err != nil {
		return ClarificationView{}, err
	}
	request, err := readWorkRequest(ctx, tx, requestID, projectID)
	if err != nil {
		return ClarificationView{}, err
	}
	view := ClarificationView{ID: request.currentClarificationID, RequestID: requestID}
	if request.currentClarificationID != "" {
		var state string
		var revision int64
		var questionMessage string
		err := tx.QueryRow(ctx, `SELECT state, revision, question_message_id FROM repomesh_messages.clarifications
			WHERE id=$1`, request.currentClarificationID).Scan(&state, &revision, &questionMessage)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return ClarificationView{}, unavailable()
		}
		if err == nil {
			view.State, view.Revision, view.QuestionMessageID = state, revision, questionMessage
			var answerMessage string
			answerErr := tx.QueryRow(ctx, `SELECT answer_message_id FROM repomesh_messages.clarification_answers
				WHERE clarification_id=$1`, request.currentClarificationID).Scan(&answerMessage)
			if answerErr == nil {
				view.AnswerMessageID = &answerMessage
			}
		}
	}
	if request.state == "resolved" {
		var resolutionID, inputKind, outcome string
		var issueID *string
		err := tx.QueryRow(ctx, `SELECT resolution_id, input_kind, outcome, issue_id FROM repomesh_messages.target_resolutions
			WHERE logical_request_id=$1`, requestID).Scan(&resolutionID, &inputKind, &outcome, &issueID)
		if err == nil {
			view.Resolution = &TargetResolutionView{ResolutionID: resolutionID, InputKind: inputKind, Outcome: outcome, IssueID: issueID}
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return ClarificationView{}, unavailable()
		}
	}
	return view, nil
}
