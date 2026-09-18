// Package gates implements P1's project checkpoint gate (Py:
// checkpoint_control.operational_gate): the 总 Leader's final review entry.
// The gate stays pending until every task is done and the leader records the
// release decision.
package gates

import (
	"context"
	crand "crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Service owns project-level release gates.
type Service struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Service { return &Service{pool: pool} }

// GateView is the project gate state.
type GateView struct {
	ProjectID string `json:"projectId"`
	State     string `json:"state"` // pending | released | rejected
	Decision  string `json:"decision,omitempty"`
	Summary   string `json:"summary,omitempty"`
	DecidedBy string `json:"decidedBy,omitempty"`
}

// Evaluate reports whether every task in the project is done.
func (s *Service) Evaluate(ctx context.Context, projectID string) (GateView, error) {
	view := GateView{ProjectID: projectID, State: "pending"}
	var pending int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM public.tasks
		WHERE project_id::text=$1 AND status IN ('pending','assigned','running','blocked')`,
		projectID).Scan(&pending); err != nil {
		return GateView{}, fmt.Errorf("gates: evaluate failed: %w", err)
	}
	if pending > 0 {
		view.Summary = fmt.Sprintf("%d task(s) still open", pending)
		return view, nil
	}
	row := s.pool.QueryRow(ctx, `SELECT result FROM repomesh_messages.control_operations
		WHERE project_id=$1 AND action='release_gate' ORDER BY created_at DESC LIMIT 1`, projectID)
	var result []byte
	if err := row.Scan(&result); err == nil && len(result) > 0 {
		var stored map[string]any
		if json.Unmarshal(result, &stored) == nil {
			view.State = stringOr(stored["state"], "pending")
			view.Decision = stringOr(stored["decision"], "")
			view.Summary = stringOr(stored["summary"], "")
			view.DecidedBy = stringOr(stored["decidedBy"], "")
		}
	}
	return view, nil
}

// Record stores the leader's release decision; refuses when work is open.
func (s *Service) Record(ctx context.Context, projectID, leaderID, decision, summary string) (GateView, error) {
	if decision != "release" && decision != "reject" {
		return GateView{}, fmt.Errorf("gates: decision must be release or reject")
	}
	view, err := s.Evaluate(ctx, projectID)
	if err != nil {
		return GateView{}, err
	}
	if view.State == "pending" && view.Summary != "" {
		return view, fmt.Errorf("gates: %s", view.Summary)
	}
	buffer := make([]byte, 10)
	if _, err := crand.Read(buffer); err != nil {
		return GateView{}, fmt.Errorf("gates: id generation failed: %w", err)
	}
	gateID := "gate_" + hex.EncodeToString(buffer)
	state := "released"
	if decision == "reject" {
		state = "rejected"
	}
	payload := map[string]any{"state": state, "decision": decision, "summary": summary, "decidedBy": leaderID}
	// canonical_input is bytea and result is jsonb: the same []byte binds as
	// bytea, so the jsonb target needs an explicit cast.
	if _, err := s.pool.Exec(ctx, `INSERT INTO repomesh_messages.control_operations
		(command_slot_id, project_id, action, schema_version, canonical_input, result)
		VALUES ($1,$2,'release_gate',1,$3,$3::jsonb)`,
		gateID, projectID, mustJSON(payload)); err != nil {
		return GateView{}, fmt.Errorf("gates: record failed: %w", err)
	}
	return GateView{ProjectID: projectID, State: state, Decision: decision, Summary: summary, DecidedBy: leaderID}, nil
}

func stringOr(value any, fallback string) string {
	if text, ok := value.(string); ok && text != "" {
		return text
	}
	return fallback
}

func mustJSON(value any) []byte {
	encoded, err := json.Marshal(value)
	if err != nil {
		return []byte("{}")
	}
	return encoded
}
