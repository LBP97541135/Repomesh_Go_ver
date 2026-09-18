// Package handoff implements P1's handoff documents: the per-plan summary a
// repository Manager produces when its work is complete, consumed by the
// Leader and by joint validation (Py: handoff_docs.py).
package handoff

import (
	"context"
	crand "crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Service stores and lists handoff documents.
type Service struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Service { return &Service{pool: pool} }

// CreateCommand submits one handoff document.
type CreateCommand struct {
	ProjectID    string   `json:"projectId"`
	PlanID       string   `json:"planId"`
	RepositoryID string   `json:"repositoryId"`
	AuthorID     string   `json:"authorId"`
	Summary      string   `json:"summary"`
	Deliverables []string `json:"deliverables"`
}

// HandoffView is the read projection.
type HandoffView struct {
	ID           string    `json:"id"`
	PlanID       string    `json:"planId"`
	RepositoryID string    `json:"repositoryId"`
	AuthorID     string    `json:"authorId"`
	Summary      string    `json:"summary"`
	Deliverables []string  `json:"deliverables"`
	CreatedAt    time.Time `json:"createdAt"`
}

// Create stores one handoff keyed by its branch_validation_key
// (plan+repository derived), making re-submission idempotent.
func (s *Service) Create(ctx context.Context, command CreateCommand) (HandoffView, error) {
	if command.PlanID == "" || command.RepositoryID == "" || command.AuthorID == "" {
		return HandoffView{}, fmt.Errorf("handoff: plan, repository and author are required")
	}
	buffer := make([]byte, 16)
	if _, err := crand.Read(buffer); err != nil {
		return HandoffView{}, fmt.Errorf("handoff: id generation failed: %w", err)
	}
	// UUIDv4: set the version (4) and variant (10xx) bits, then slice the 32
	// hex chars into canonical 8-4-4-4-12 groups. The previous format injected
	// an extra "4" and "8" into 4-char groups, producing a 38-char string the
	// uuid column rejects outright.
	buffer[6] = (buffer[6] & 0x0f) | 0x40
	buffer[8] = (buffer[8] & 0x3f) | 0x80
	hexSum := hex.EncodeToString(buffer)
	id := fmt.Sprintf("%s-%s-%s-%s-%s", hexSum[0:8], hexSum[8:12], hexSum[12:16], hexSum[16:20], hexSum[20:32])
	key := fmt.Sprintf("%s:%s", command.PlanID, command.RepositoryID)
	payload, _ := json.Marshal(map[string]any{
		"planId": command.PlanID, "repositoryId": command.RepositoryID,
		"authorId": command.AuthorID, "summary": command.Summary,
		"deliverables": command.Deliverables,
	})
	var createdAt time.Time
	err := s.pool.QueryRow(ctx, `INSERT INTO public.handoffs
		(id, task_id, branch_validation_key, payload, evidence, status, created_at)
		VALUES ($1, $2::uuid, $3, $4::jsonb, '{}'::jsonb, 'submitted', clock_timestamp())
		ON CONFLICT (branch_validation_key) DO UPDATE SET payload = EXCLUDED.payload, status='submitted'
		RETURNING created_at`,
		id, zeroTaskUUID(), key, payload).Scan(&createdAt)
	if err != nil {
		return HandoffView{}, fmt.Errorf("handoff: insert failed: %w", err)
	}
	return HandoffView{
		ID: id, PlanID: command.PlanID, RepositoryID: command.RepositoryID,
		AuthorID: command.AuthorID, Summary: command.Summary,
		Deliverables: command.Deliverables, CreatedAt: createdAt,
	}, nil
}

// zeroTaskUUID satisfies handoffs.task_id NOT NULL for plan-level handoffs
// that are not bound to a single task; it is the nil UUID.
func zeroTaskUUID() string { return "00000000-0000-0000-0000-000000000000" }

// List returns the handoffs of one plan, newest first.
func (s *Service) List(ctx context.Context, planID string) ([]HandoffView, error) {
	rows, err := s.pool.Query(ctx, `SELECT id::text, payload, created_at FROM public.handoffs
		WHERE branch_validation_key LIKE $1 || ':%' ORDER BY created_at DESC`, planID)
	if err != nil {
		return nil, fmt.Errorf("handoff: query failed: %w", err)
	}
	defer rows.Close()
	result := []HandoffView{}
	for rows.Next() {
		var item HandoffView
		var payload []byte
		if rows.Scan(&item.ID, &payload, &item.CreatedAt) != nil {
			return nil, fmt.Errorf("handoff: scan failed")
		}
		var stored map[string]any
		if json.Unmarshal(payload, &stored) == nil {
			item.PlanID = stringOr(stored["planId"])
			item.RepositoryID = stringOr(stored["repositoryId"])
			item.AuthorID = stringOr(stored["authorId"])
			item.Summary = stringOr(stored["summary"])
			if items, ok := stored["deliverables"].([]any); ok {
				for _, entry := range items {
					if text, ok := entry.(string); ok {
						item.Deliverables = append(item.Deliverables, text)
					}
				}
			}
		}
		if item.Deliverables == nil {
			item.Deliverables = []string{}
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func stringOr(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	return ""
}
