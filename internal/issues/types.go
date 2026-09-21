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
	// hitlMode 是这次 issue 的人审门模式：ai = 自动托管（处理员代行 ③ 分档审批
	// 与 ⑤ 物化确认），hitl = 门等真人。它是**服务端事实**（0053 迁移落列），
	// 协调器按它停门 —— 此前它只活在浏览器 sessionStorage 里，Go 侧看不到。
	hitlMode string
	// mergeMode 是这次 issue 的**合并方式**：auto = 交付闸门一开就自动把 PR 合掉，
	// manual = 等人在交付序列上逐条点合并（缺省）。
	//
	// 2026-09-21 用户裁定："pr 合并也做出可选择项目"。合并是整条链上唯一的外部
	// 副作用，所以它必须是一个**显式的、可追溯的选择**，而不是硬编码的行为。
	mergeMode string
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
