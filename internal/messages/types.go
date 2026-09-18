// Package messages implements the local half of the Manager conversation
// pipeline (G1/G2): idempotent message submission under per-conversation
// sequence allocation, the clarification state machine, and target
// resolution. Per backend-message-clarification-design.md.
package messages

import (
	"github.com/jackc/pgx/v5/pgxpool"
	"repomesh.local/repomesh/internal/access"
	"repomesh.local/repomesh/internal/issues"
)

// MessageService owns conversation message persistence and interpretation
// records. Model transport is deliberately absent: no outbound model call is
// made here, and unknown integration stays blocked rather than faked.
type MessageService struct {
	pool          *pgxpool.Pool
	authorization *access.Service
	issues        *issues.Service
}

func NewMessageService(pool *pgxpool.Pool, authorization *access.Service, issueService *issues.Service) *MessageService {
	return &MessageService{pool: pool, authorization: authorization, issues: issueService}
}

// SubmissionCommand is one authenticated message send from the web layer.
type SubmissionCommand struct {
	ProjectID      string
	ConversationID string
	SubmissionID   string
	Actor          string
	Body           []byte
}
