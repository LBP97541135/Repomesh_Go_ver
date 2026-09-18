package interfacedoc

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Create submits a new document version. The author is added to approvers
// implicitly by not needing approval; every other manager in Approvers must
// approve. Content is required and stored verbatim.
func (s *Service) Create(ctx context.Context, command CreateCommand) (DocumentView, error) {
	if command.ProjectID == "" || command.AuthorID == "" || strings.TrimSpace(command.Content) == "" {
		return DocumentView{}, fmt.Errorf("interfacedoc: project, author and content are required")
	}
	deduped := dedupe(command.Approvers, command.AuthorID)
	docID, err := newID("ifd_")
	if err != nil {
		return DocumentView{}, err
	}
	docVersion := time.Now().Unix() % 100000
	if _, err := s.pool.Exec(ctx, `INSERT INTO repomesh_messages.control_operations
		(command_slot_id, project_id, action, schema_version, canonical_input)
		VALUES ($1,$2,'interface_document',1,$3)`,
		docID, command.ProjectID, mustJSON(map[string]any{
			"issueId": command.IssueID, "version": docVersion, "title": command.Title,
			"content": command.Content, "author": command.AuthorID, "approvers": deduped,
		})); err != nil {
		return DocumentView{}, wrap(err)
	}
	return DocumentView{
		ID:         docID,
		Version:    int(docVersion),
		Title:      command.Title,
		Content:    command.Content,
		State:      "awaiting_approval",
		AuthorID:   command.AuthorID,
		Approvers:  deduped,
		ApprovedBy: []string{},
	}, nil
}

// Approve records one manager's approval; when all approvers have approved
// the document flips to effective. Idempotent per approver: a second approval
// by the same manager is a no-op returning the current state.
func (s *Service) Approve(ctx context.Context, documentID, managerID string) (DocumentView, error) {
	row := s.pool.QueryRow(ctx, `SELECT canonical_input, result FROM repomesh_messages.control_operations
		WHERE command_slot_id=$1 AND action='interface_document'`, documentID)
	var canonical, existing []byte
	if err := row.Scan(&canonical, &existing); err != nil {
		return DocumentView{}, unavailable()
	}
	var payload map[string]any
	if err := json.Unmarshal(canonical, &payload); err != nil {
		return DocumentView{}, unavailable()
	}
	approvers := toStringSlice(payload["approvers"])
	var approved []string
	if len(existing) > 0 {
		var stored map[string]any
		if json.Unmarshal(existing, &stored) == nil {
			approved = toStringSlice(stored["approvedBy"])
		}
	}
	if approved == nil {
		approved = toStringSlice(payload["approvedBy"])
	}
	if !contains(approvers, managerID) {
		return DocumentView{}, fmt.Errorf("interfacedoc: manager %s is not an approver", managerID)
	}
	if !contains(approved, managerID) {
		approved = append(approved, managerID)
	}
	allApproved := len(approved) >= len(approvers)
	payload["approvedBy"] = approved
	effectiveAt := ""
	state := "awaiting_approval"
	if allApproved {
		state = "effective"
		effectiveAt = nowRFC3339()
		payload["effectiveAt"] = effectiveAt
	}
	if _, err := s.pool.Exec(ctx, `UPDATE repomesh_messages.control_operations SET result=$2::jsonb
		WHERE command_slot_id=$1`, documentID, mustJSON(payload)); err != nil {
		return DocumentView{}, unavailable()
	}
	view := DocumentView{
		ID:         documentID,
		Title:      toString(payload["title"]),
		Content:    toString(payload["content"]),
		State:      state,
		AuthorID:   toString(payload["author"]),
		Approvers:  approvers,
		ApprovedBy: approved,
	}
	if effectiveAt != "" {
		view.EffectiveAt = effectiveAt
	}
	return view, nil
}

// Get returns the document projection.
func (s *Service) Get(ctx context.Context, documentID string) (DocumentView, error) {
	var canonical, result []byte
	err := s.pool.QueryRow(ctx, `SELECT canonical_input, result FROM repomesh_messages.control_operations
		WHERE command_slot_id=$1 AND action='interface_document'`, documentID).Scan(&canonical, &result)
	if err != nil {
		return DocumentView{}, unavailable()
	}
	var payload map[string]any
	if err := json.Unmarshal(canonical, &payload); err != nil {
		return DocumentView{}, unavailable()
	}
	view := DocumentView{
		ID:        documentID,
		Version:   intOf(payload["version"]),
		Title:     toString(payload["title"]),
		Content:   toString(payload["content"]),
		AuthorID:  toString(payload["author"]),
		Approvers: toStringSlice(payload["approvers"]),
		State:     "awaiting_approval",
	}
	if result != nil {
		var res map[string]any
		if json.Unmarshal(result, &res) == nil {
			view.ApprovedBy = toStringSlice(res["approvedBy"])
			view.EffectiveAt = toString(res["effectiveAt"])
			if view.EffectiveAt != "" {
				view.State = "effective"
			}
		}
	}
	if view.ApprovedBy == nil {
		view.ApprovedBy = []string{}
	}
	return view, nil
}

func dedupe(values []string, exclude string) []string {
	seen := map[string]bool{}
	result := []string{}
	for _, value := range values {
		if value == "" || value == exclude || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func toString(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	return ""
}

func intOf(value any) int {
	if number, ok := value.(float64); ok {
		return int(number)
	}
	return 0
}

func toStringSlice(value any) []string {
	items, ok := value.([]any)
	if !ok {
		return []string{}
	}
	result := make([]string, 0, len(items))
	for _, item := range items {
		if text, ok := item.(string); ok {
			result = append(result, text)
		}
	}
	return result
}

func mustJSON(value any) []byte {
	encoded, err := json.Marshal(value)
	if err != nil {
		return []byte("{}")
	}
	return encoded
}

func newID(prefix string) (string, error) {
	buffer := make([]byte, 10)
	if _, err := rand.Read(buffer); err != nil {
		return "", unavailable()
	}
	return prefix + hex.EncodeToString(buffer), nil
}

func nowRFC3339() string {
	return time.Now().UTC().Format(time.RFC3339)
}
