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

	// reviews 是审核台的写面（组合根接 humancontrol.Service）。
	// Nil = 未接线：规格变更请求只落库、**不落审核台** —— 调用方据 Submission.ReviewID
	// 为空如实告知"没人能批"，不假装已经进了人工队列。
	reviews ReviewSink
}

func New(pool *pgxpool.Pool) *Service { return &Service{pool: pool} }

// WithReviews attaches the review desk (composition root).
func (s *Service) WithReviews(sink ReviewSink) *Service {
	s.reviews = sink
	return s
}

// CreateCommand submits one spec version.
type CreateCommand struct {
	ProjectID  string `json:"projectId"`
	Repository string `json:"repository"`
	AuthorID   string `json:"authorId"`
	Title      string `json:"title"`
	Content    string `json:"content"`
	Supersedes string `json:"supersedes,omitempty"`
	// Origin 记这一版规格**从哪来**（如 "spec-change-request:<evidence>"）——
	// agent 提的变更经人批之后落成规格，出处必须留在规格本身里，否则
	// "这版是谁提的"只能去审核台反查。
	Origin string `json:"origin,omitempty"`
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
	version, err := s.NextVersion(ctx, command.ProjectID, command.Repository)
	if err != nil {
		return SpecView{}, err
	}
	payload := map[string]any{
		"repository": command.Repository, "version": version, "title": command.Title,
		"content": command.Content, "author": command.AuthorID, "state": "draft",
		"supersedes": command.Supersedes, "origin": command.Origin,
	}
	if _, err := s.pool.Exec(ctx, `INSERT INTO repomesh_messages.control_operations
		(command_slot_id, project_id, action, schema_version, canonical_input)
		VALUES ($1,$2,'specification',1,$3)`,
		id, command.ProjectID, mustJSON(payload)); err != nil {
		return SpecView{}, fmt.Errorf("spec: insert failed: %w", err)
	}
	return SpecView{ID: id, Version: version, Title: command.Title, Content: command.Content, State: "draft", AuthorID: command.AuthorID}, nil
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

// NextVersion 返回该仓库下一个规格版本号（该仓库已出现的最大版本 + 1；没有则 1）。
//
// 2026-09-20：Create 此前把 version 硬编码成 1 —— 于是"规格升版"这件事在数据上
// 根本不成立：每一份新规格都是 v1，Current() 只按 repository 过滤、取版本最大的
// 那一份，升版后界面也看不出换代。A2 的回路（人批 → 规格升版 → 触发重规划）要的
// 正是这个版本轴，所以先把它补上。
func (s *Service) NextVersion(ctx context.Context, projectID, repository string) (int, error) {
	rows, err := s.pool.Query(ctx, `SELECT canonical_input, result FROM repomesh_messages.control_operations
		WHERE project_id=$1 AND action='specification'`, projectID)
	if err != nil {
		return 0, fmt.Errorf("spec: version query failed: %w", err)
	}
	defer rows.Close()
	max := 0
	for rows.Next() {
		var canonical, result []byte
		if rows.Scan(&canonical, &result) != nil {
			return 0, fmt.Errorf("spec: version scan failed")
		}
		source := canonical
		if len(result) > 0 {
			source = result
		}
		var payload map[string]any
		if json.Unmarshal(source, &payload) != nil {
			continue
		}
		if toString(payload["repository"]) != repository {
			continue
		}
		if version := intOf(payload["version"]); version > max {
			max = version
		}
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("spec: version rows: %w", err)
	}
	return max + 1, nil
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
