// Package issues implements the atomic creation of issues from the issue page:
// idempotent page commands, conversation + issue + main changeset + source card
// in one short transaction, per the issue-page-create API contract.
package issues

import (
	"strings"
	"unicode/utf8"
)

// IssueID, ConversationID, ChangeSetID and OperationID are opaque stable identifiers.
type (
	IssueID        = string
	ConversationID = string
	ChangeSetID    = string
	OperationID    = string
)

type conversationChoice interface{ conversationMode() string }

type newConversation struct{}

func (newConversation) conversationMode() string { return "new" }

type existingConversation struct{ id string }

func (existingConversation) conversationMode() string { return "existing" }

// pageInput is the parsed, validated page-create input. Canonical equality is
// computed over this struct's normalized fields.
type pageInput struct {
	expectedContext string
	title           string
	description     string
	repositories    []string
	criteria        []string
	conversation    conversationChoice
	analysisID      *string
}

// PageCommand carries one authenticated page-create command from the web layer.
type PageCommand struct {
	ProjectID string
	Key       OperationID
	RequestID string
	Body      []byte
}

// ParsePageCommand validates the command envelope.
func ParsePageCommand(projectID, key, requestID string, body []byte) (PageCommand, error) {
	if err := validProjectID(projectID); err != nil {
		return PageCommand{}, err
	}
	if !utf8.ValidString(key) || !utf8.ValidString(requestID) {
		return PageCommand{}, failure(400, "INVALID_IDEMPOTENCY_KEY")
	}
	if len(key) < 16 || len(key) > 128 {
		return PageCommand{}, failure(400, "INVALID_IDEMPOTENCY_KEY")
	}
	return PageCommand{ProjectID: projectID, Key: key, RequestID: requestID, Body: body}, nil
}

func validProjectID(raw string) error {
	if raw == "" || !utf8.ValidString(raw) || len(raw) > 64 || strings.ContainsAny(raw, "/%\\?#\x00") {
		return failure(404, "RESOURCE_NOT_FOUND")
	}
	return nil
}
