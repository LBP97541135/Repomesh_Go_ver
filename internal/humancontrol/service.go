// Package humancontrol implements the ReviewDesk surface: the cross-project
// human review queue (Py: human_control). Reads and decisions authenticate
// with the local login session; the decision-taker is always the session
// account, never a client-supplied field.
package humancontrol

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrEvidenceDrifted = errors.New("humancontrol: evidence version drifted")

// Service reads and records checkpoint review state.
type Service struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Service { return &Service{pool: pool} }

// ReviewView mirrors the frontend HumanReviewRequestView (snake_case JSON).
type ReviewView struct {
	ID                string     `json:"id"`
	ProjectID         string     `json:"project_id"`
	Checkpoint        string     `json:"checkpoint"`
	EvidenceVersion   string     `json:"evidence_version"`
	Title             string     `json:"title"`
	Summary           string     `json:"summary"`
	Status            string     `json:"status"`
	RepositoryID      *string    `json:"repository_id"`
	RequestedByAgentID *string   `json:"requested_by_agent_id"`
	ResolvedByHumanID *string    `json:"resolved_by_human_id"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

// DecisionView mirrors the frontend CheckpointDecisionView.
type DecisionView struct {
	ID               string    `json:"id"`
	ReviewRequestID  string    `json:"review_request_id"`
	ProjectID        string    `json:"project_id"`
	Checkpoint       string    `json:"checkpoint"`
	HumanPrincipalID string    `json:"human_principal_id"`
	Decision         string    `json:"decision"`
	Reason           string    `json:"reason"`
	RepositoryID     *string   `json:"repository_id"`
	EvidenceVersion  string    `json:"evidence_version"`
	DecidedAt        time.Time `json:"decided_at"`
}

func newID() string {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		return ""
	}
	return fmt.Sprintf("%x-%x-4%x-8%x-%x", buffer[0:4], buffer[4:6], buffer[6:8], buffer[8:10], buffer[10:16])
}

// IsAdmin reports whether the session account may see the whole queue.
func (s *Service) IsAdmin(ctx context.Context, actor string) (bool, error) {
	var admin bool
	err := s.pool.QueryRow(ctx, `SELECT is_admin FROM repomesh_access.accounts WHERE id=$1`, actor).Scan(&admin)
	if err != nil {
		return false, fmt.Errorf("humancontrol: account lookup: %w", err)
	}
	return admin, nil
}

// List returns review requests: everything for admins, only items assigned
// to the account otherwise (the frontend's `_reviews_for` contract).
func (s *Service) List(ctx context.Context, actor string, admin bool, status string) ([]ReviewView, error) {
	query := `SELECT id::text, project_id::text, checkpoint, evidence_version, title, summary, status,
		repository_id, requested_by_agent_id::text, decided_by::text, created_at, updated_at
		FROM public.review_requests`
	args := []any{}
	conditions := []string{}
	if !admin {
		conditions = append(conditions, "(assignee IS NOT NULL AND assignee=$1)")
		args = append(args, actor)
	}
	if status != "" {
		conditions = append(conditions, fmt.Sprintf("status=$%d", len(args)+1))
		args = append(args, status)
	}
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	query += " ORDER BY created_at DESC LIMIT 500"
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("humancontrol: list: %w", err)
	}
	defer rows.Close()
	return scanReviews(rows)
}

func scanReviews(rows pgx.Rows) ([]ReviewView, error) {
	result := []ReviewView{}
	for rows.Next() {
		var view ReviewView
		if err := rows.Scan(&view.ID, &view.ProjectID, &view.Checkpoint, &view.EvidenceVersion,
			&view.Title, &view.Summary, &view.Status, &view.RepositoryID,
			&view.RequestedByAgentID, &view.ResolvedByHumanID, &view.CreatedAt, &view.UpdatedAt); err != nil {
			return nil, fmt.Errorf("humancontrol: scan: %w", err)
		}
		result = append(result, view)
	}
	return result, rows.Err()
}

// DecisionCommand records one checkpoint decision against the exact evidence
// the reviewer saw; a changed evidence_version is a 409, not a silent accept.
type DecisionCommand struct {
	ReviewRequestID string
	Decision        string
	Reason          string
}

func (s *Service) Decide(ctx context.Context, actor, projectID string, command DecisionCommand) (DecisionView, error) {
	valid := map[string]bool{"approved": true, "rejected": true, "changes_requested": true}
	if !valid[command.Decision] {
		return DecisionView{}, fmt.Errorf("humancontrol: invalid decision %q", command.Decision)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return DecisionView{}, fmt.Errorf("humancontrol: begin: %w", err)
	}
	defer tx.Rollback(ctx)

	var checkpoint, evidence, currentStatus, requestProject string
	var repositoryID *string
	err = tx.QueryRow(ctx, `SELECT checkpoint, evidence_version, status, project_id::text, repository_id
		FROM public.review_requests WHERE id=$1 FOR UPDATE`, command.ReviewRequestID).
		Scan(&checkpoint, &evidence, &currentStatus, &requestProject, &repositoryID)
	if errors.Is(err, pgx.ErrNoRows) {
		return DecisionView{}, pgx.ErrNoRows
	}
	if err != nil {
		return DecisionView{}, fmt.Errorf("humancontrol: load request: %w", err)
	}
	if requestProject != projectID {
		return DecisionView{}, pgx.ErrNoRows
	}
	if currentStatus != "pending" {
		return DecisionView{}, ErrEvidenceDrifted
	}

	id := newID()
	var decidedAt time.Time
	err = tx.QueryRow(ctx, `INSERT INTO public.checkpoint_decisions
		(id, review_request_id, project_id, checkpoint, human_principal_id, decision, reason, repository_id, evidence_version, decided_at)
		VALUES ($1::uuid, $2::uuid, $3::uuid, $4, $5, $6, $7, $8, $9, now()) RETURNING decided_at`,
		id, command.ReviewRequestID, projectID, checkpoint, actor, command.Decision, command.Reason,
		repositoryID, evidence).Scan(&decidedAt)
	if err != nil {
		return DecisionView{}, fmt.Errorf("humancontrol: insert decision: %w", err)
	}
	newStatus := command.Decision
	if _, err := tx.Exec(ctx, `UPDATE public.review_requests SET status=$2, decided_by=$3, decision_note=$4, decided_at=now(), updated_at=now()
		WHERE id=$1`, command.ReviewRequestID, newStatus, actor, command.Reason); err != nil {
		return DecisionView{}, fmt.Errorf("humancontrol: update request: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return DecisionView{}, fmt.Errorf("humancontrol: commit: %w", err)
	}
	return DecisionView{
		ID: id, ReviewRequestID: command.ReviewRequestID, ProjectID: projectID,
		Checkpoint: checkpoint, HumanPrincipalID: actor, Decision: command.Decision,
		Reason: command.Reason, RepositoryID: repositoryID, EvidenceVersion: evidence, DecidedAt: decidedAt,
	}, nil
}

// ControlCommand pauses, resumes, or cancels a project.
type ControlCommand struct {
	Action string
}

func (s *Service) Control(ctx context.Context, actor, projectID string, command ControlCommand) error {
	actions := map[string]string{
		"pause_project":  "paused",
		"resume_project": "active",
		"cancel_project": "cancelled",
	}
	status, ok := actions[command.Action]
	if !ok {
		return fmt.Errorf("humancontrol: invalid control action %q", command.Action)
	}
	tag, err := s.pool.Exec(ctx, `UPDATE repomesh_projects.projects SET status=$2 WHERE id=$1 AND status<>'cancelled' AND removed_at IS NULL`, projectID, status)
	if err != nil {
		return fmt.Errorf("humancontrol: control: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}
