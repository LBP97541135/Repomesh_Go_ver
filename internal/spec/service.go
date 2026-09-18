// Package spec implements P1's Manager-authored specification lifecycle
// (Py: modules/specification SpecificationService create/revise/submit/
// approve). Documents are immutable versions; approving a submitted version
// publishes it as the repository's current spec that task packages carry.
package spec

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Service owns spec documents. Storage reuses repomesh_messages
// .control_operations with action='specification' (idempotent slots, JSON
// payload, same pattern as interface documents).
type Service struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Service { return &Service{pool: pool} }

// CreateCommand submits one spec version.
type CreateCommand struct {
	ProjectID  string `json:"projectId"`
	Repository string `json:"repository"`
	AuthorID   string `json:"authorId"`
	Title      string `json:"title"`
	Content    string `json:"content"`
	Supersedes string `json:"supersedes,omitempty"`
}

// SpecView is the read projection.
type SpecView struct {
	ID       string `json:"id"`
	Version  int    `json:"version"`
	Title    string `json:"title"`
	Content  string `json:"content"`
	State    string `json:"state"` // draft | approved
	AuthorID string `json:"authorId"`
}

// Create stores a draft spec.
func (s *Service) Create(ctx context.Context, command CreateCommand) (SpecView, error) {
	if command.ProjectID == "" || command.AuthorID == "" || command.Repository == "" || command.Content == "" {
		return SpecView{}, fmt.Errorf("spec: project, author, repository and content are required")
	}
	id, err := newID("spec_")
	if err != nil {
		return SpecView{}, err
	}
	payload := map[string]any{
		"repository": command.Repository, "version": 1, "title": command.Title,
		"content": command.Content, "author": command.AuthorID, "state": "draft",
		"supersedes": command.Supersedes,
	}
	if _, err := s.pool.Exec(ctx, `INSERT INTO repomesh_messages.control_operations
		(command_slot_id, project_id, action, schema_version, canonical_input)
		VALUES ($1,$2,'specification',1,$3)`,
		id, command.ProjectID, mustJSON(payload)); err != nil {
		return SpecView{}, fmt.Errorf("spec: insert failed: %w", err)
	}
	return SpecView{ID: id, Version: 1, Title: command.Title, Content: command.Content, State: "draft", AuthorID: command.AuthorID}, nil
}

// Approve publishes a draft (manager action); the approved content becomes
// the current spec for its repository.
func (s *Service) Approve(ctx context.Context, specID, approverID string) (SpecView, error) {
	row := s.pool.QueryRow(ctx, `SELECT canonical_input FROM repomesh_messages.control_operations
		WHERE command_slot_id=$1 AND action='specification'`, specID)
	var canonical []byte
	if err := row.Scan(&canonical); err != nil {
		return SpecView{}, fmt.Errorf("spec: lookup failed: %w", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(canonical, &payload); err != nil {
		return SpecView{}, fmt.Errorf("spec: payload unreadable")
	}
	if toString(payload["state"]) != "draft" {
		return SpecView{}, fmt.Errorf("spec: only draft specs can be approved")
	}
	payload["state"] = "approved"
	payload["approvedBy"] = approverID
	if _, err := s.pool.Exec(ctx, `UPDATE repomesh_messages.control_operations SET result=$2
		WHERE command_slot_id=$1`, specID, mustJSON(payload)); err != nil {
		return SpecView{}, fmt.Errorf("spec: approve failed: %w", err)
	}
	return SpecView{
		ID: specID, Version: intOf(payload["version"]), Title: toString(payload["title"]),
		Content: toString(payload["content"]), State: "approved", AuthorID: toString(payload["author"]),
	}, nil
}

// Current returns the approved spec of one repository, if any.
func (s *Service) Current(ctx context.Context, projectID, repository string) (SpecView, error) {
	rows, err := s.pool.Query(ctx, `SELECT canonical_input, result FROM repomesh_messages.control_operations
		WHERE project_id=$1 AND action='specification'`, projectID)
	if err != nil {
		return SpecView{}, fmt.Errorf("spec: query failed: %w", err)
	}
	defer rows.Close()
	latest := SpecView{State: "none"}
	for rows.Next() {
		var canonical, result []byte
		if rows.Scan(&canonical, &result) != nil {
			return SpecView{}, fmt.Errorf("spec: scan failed")
		}
		var payload map[string]any
		source := canonical
		if len(result) > 0 {
			source = result
		}
		if json.Unmarshal(source, &payload) != nil {
			continue
		}
		if toString(payload["repository"]) != repository {
			continue
		}
		if toString(payload["state"]) != "approved" {
			continue
		}
		version := intOf(payload["version"])
		if version >= latest.Version {
			latest = SpecView{
				ID: "", Version: version, Title: toString(payload["title"]),
				Content: toString(payload["content"]), State: "approved", AuthorID: toString(payload["author"]),
			}
		}
	}
	// recover the id of the latest approved spec
	if latest.State == "approved" {
		_ = s.pool.QueryRow(ctx, `SELECT command_slot_id FROM repomesh_messages.control_operations
			WHERE project_id=$1 AND action='specification' ORDER BY created_at DESC LIMIT 1`, projectID).Scan(&latest.ID)
	}
	return latest, nil
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
		return "", fmt.Errorf("spec: id generation failed: %w", err)
	}
	return prefix + hex.EncodeToString(buffer), nil
}
