// Package responsibility implements E 跨仓职责 / 授权 / 冲突:
// case timeline, owner confirmation, authorization request/revoke,
// responsibility transfer, conflict resolution, and platform status sync.
package responsibility

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Service struct{ Pool *pgxpool.Pool }

func New(pool *pgxpool.Pool) *Service { return &Service{Pool: pool} }

// --- Case timeline ---------------------------------------------------------

type CaseEvent struct {
	ID        string          `json:"id"`
	PlanID    string          `json:"plan_id"`
	IssueID   *string         `json:"issue_id"`
	ProjectID string          `json:"project_id"`
	ActorRole string          `json:"actor_role"`
	ActorID   string          `json:"actor_id"`
	EventKind string          `json:"event_kind"`
	Detail    json.RawMessage `json:"detail"`
	CreatedAt time.Time       `json:"created_at"`
}

func (s *Service) RecordEvent(ctx context.Context, planID, issueID, projectID, actorRole, actorID, eventKind string, detail map[string]any) (*CaseEvent, error) {
	blob, _ := json.Marshal(detail)
	row := s.Pool.QueryRow(ctx, `
		INSERT INTO public.case_events (plan_id, issue_id, project_id, actor_role, actor_id, event_kind, detail)
		-- 注意：issue_id 不要加 ::uuid。issue id 形如 iss_xxx
		-- （repomesh_issues.issues.id 是 text），加了就是 22P02；
		-- 0059 迁移已把这一列改成 text。
		VALUES ($1, NULLIF($2,''), $3, $4, $5, $6, $7::jsonb)
		RETURNING id, plan_id::text, issue_id::text, project_id::text, actor_role, actor_id, event_kind, detail, created_at`,
		planID, issueID, projectID, actorRole, actorID, eventKind, string(blob))
	return scanCaseEvent(row)
}

func (s *Service) Timeline(ctx context.Context, planID string) ([]CaseEvent, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT id, plan_id::text, issue_id::text, project_id::text, actor_role, actor_id, event_kind, detail, created_at
		FROM public.case_events WHERE plan_id = $1 ORDER BY created_at`, planID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CaseEvent
	for rows.Next() {
		e, err := scanCaseEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *e)
	}
	return out, rows.Err()
}

func scanCaseEvent(row interface{ Scan(...any) error }) (*CaseEvent, error) {
	e := &CaseEvent{}
	var detail string
	if err := row.Scan(&e.ID, &e.PlanID, &e.IssueID, &e.ProjectID, &e.ActorRole, &e.ActorID, &e.EventKind, &detail, &e.CreatedAt); err != nil {
		return nil, err
	}
	e.Detail = json.RawMessage(detail)
	return e, nil
}

// --- Owner confirmation ----------------------------------------------------

type OwnerConfirmation struct {
	ID           string     `json:"id"`
	PlanID       string     `json:"plan_id"`
	RepositoryID string     `json:"repository_id"`
	OwnerGithub  string     `json:"owner_github_id"`
	ConfirmedBy  string     `json:"confirmed_by"`
	Status       string     `json:"status"`
	ConfirmedAt  *time.Time `json:"confirmed_at"`
	CreatedAt    time.Time  `json:"created_at"`
}

func (s *Service) ConfirmOwner(ctx context.Context, planID, repoID, ownerGithub, confirmedBy string) (*OwnerConfirmation, error) {
	row := s.Pool.QueryRow(ctx, `
		INSERT INTO public.repo_owner_confirmations (plan_id, repository_id, owner_github_id, confirmed_by, status, confirmed_at)
		VALUES ($1, $2, $3, $4, 'confirmed', now())
		ON CONFLICT (plan_id, repository_id) DO UPDATE SET
			owner_github_id=$3, confirmed_by=$4, status='confirmed', confirmed_at=now()
		RETURNING id, plan_id::text, repository_id::text, owner_github_id, confirmed_by, status, confirmed_at, created_at`,
		planID, repoID, ownerGithub, confirmedBy)
	return scanOwner(row)
}

func scanOwner(row interface{ Scan(...any) error }) (*OwnerConfirmation, error) {
	o := &OwnerConfirmation{}
	return o, row.Scan(&o.ID, &o.PlanID, &o.RepositoryID, &o.OwnerGithub, &o.ConfirmedBy, &o.Status, &o.ConfirmedAt, &o.CreatedAt)
}

// --- Authorization request / grant / revoke --------------------------------

type AuthRequest struct {
	ID           string     `json:"id"`
	PlanID       string     `json:"plan_id"`
	RepositoryID string     `json:"repository_id"`
	RequesterID  string     `json:"requester_id"`
	RequesterRole string    `json:"requester_role"`
	Scope        string     `json:"scope"`
	Status       string     `json:"status"`
	GrantedBy    *string    `json:"granted_by"`
	GrantedAt    *time.Time `json:"granted_at"`
	RevokedBy    *string    `json:"revoked_by"`
	RevokedAt    *time.Time `json:"revoked_at"`
	Reason       string     `json:"reason"`
	CreatedAt    time.Time  `json:"created_at"`
}

func (s *Service) RequestAuth(ctx context.Context, planID, repoID, requesterID, requesterRole, scope, reason string) (*AuthRequest, error) {
	row := s.Pool.QueryRow(ctx, `
		INSERT INTO public.authorization_requests (plan_id, repository_id, requester_id, requester_role, scope, reason)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, plan_id::text, repository_id::text, requester_id, requester_role, scope, status,
			granted_by, granted_at, revoked_by, revoked_at, reason, created_at`,
		planID, repoID, requesterID, requesterRole, scope, reason)
	return scanAuth(row)
}

func (s *Service) GrantAuth(ctx context.Context, id, grantedBy string) (*AuthRequest, error) {
	row := s.Pool.QueryRow(ctx, `
		UPDATE public.authorization_requests SET status='granted', granted_by=$2, granted_at=now()
		WHERE id=$1 AND status='pending'
		RETURNING id, plan_id::text, repository_id::text, requester_id, requester_role, scope, status,
			granted_by, granted_at, revoked_by, revoked_at, reason, created_at`,
		id, grantedBy)
	return scanAuth(row)
}

func (s *Service) RevokeAuth(ctx context.Context, id, revokedBy string) (*AuthRequest, error) {
	row := s.Pool.QueryRow(ctx, `
		UPDATE public.authorization_requests SET status='revoked', revoked_by=$2, revoked_at=now()
		WHERE id=$1 AND status='granted'
		RETURNING id, plan_id::text, repository_id::text, requester_id, requester_role, scope, status,
			granted_by, granted_at, revoked_by, revoked_at, reason, created_at`,
		id, revokedBy)
	return scanAuth(row)
}

func scanAuth(row interface{ Scan(...any) error }) (*AuthRequest, error) {
	a := &AuthRequest{}
	return a, row.Scan(&a.ID, &a.PlanID, &a.RepositoryID, &a.RequesterID, &a.RequesterRole, &a.Scope,
		&a.Status, &a.GrantedBy, &a.GrantedAt, &a.RevokedBy, &a.RevokedAt, &a.Reason, &a.CreatedAt)
}

// --- Responsibility transfer ------------------------------------------------

type Transfer struct {
	ID         string    `json:"id"`
	PlanID     string    `json:"plan_id"`
	FromRole   string    `json:"from_role"`
	FromID     string    `json:"from_id"`
	ToRole     string    `json:"to_role"`
	ToID       string    `json:"to_id"`
	Reason     string    `json:"reason"`
	TransferredAt time.Time `json:"transferred_at"`
}

func (s *Service) Transfer(ctx context.Context, planID, fromRole, fromID, toRole, toID, reason string) (*Transfer, error) {
	row := s.Pool.QueryRow(ctx, `
		INSERT INTO public.responsibility_transfers (plan_id, from_role, from_id, to_role, to_id, reason)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, plan_id::text, from_role, from_id, to_role, to_id, reason, transferred_at`,
		planID, fromRole, fromID, toRole, toID, reason)
	t := &Transfer{}
	return t, row.Scan(&t.ID, &t.PlanID, &t.FromRole, &t.FromID, &t.ToRole, &t.ToID, &t.Reason, &t.TransferredAt)
}

// --- Conflict resolution -----------------------------------------------------

type Conflict struct {
	ID           string     `json:"id"`
	PlanID       string     `json:"plan_id"`
	ConflictType string     `json:"conflict_type"`
	DiscoveredBy string     `json:"discovered_by"`
	DiscoveredByRole string `json:"discovered_by_role"`
	ResolvedBy   *string    `json:"resolved_by"`
	ResolvedByRole *string  `json:"resolved_by_role"`
	Resolution   string     `json:"resolution"`
	Status       string     `json:"status"`
	ResolvedAt   *time.Time `json:"resolved_at"`
	CreatedAt    time.Time  `json:"created_at"`
}

func (s *Service) ReportConflict(ctx context.Context, planID, conflictType, discoveredBy, discoveredByRole string) (*Conflict, error) {
	row := s.Pool.QueryRow(ctx, `
		INSERT INTO public.conflict_resolutions (plan_id, conflict_type, discovered_by, discovered_by_role)
		VALUES ($1, $2, $3, $4)
		RETURNING id, plan_id::text, conflict_type, discovered_by, discovered_by_role,
			resolved_by, resolved_by_role, resolution, status, resolved_at, created_at`,
		planID, conflictType, discoveredBy, discoveredByRole)
	return scanConflict(row)
}

func (s *Service) ResolveConflict(ctx context.Context, id, resolvedBy, resolvedByRole, resolution string) (*Conflict, error) {
	row := s.Pool.QueryRow(ctx, `
		UPDATE public.conflict_resolutions SET status='resolved', resolved_by=$2, resolved_by_role=$3,
			resolution=$4, resolved_at=now()
		WHERE id=$1 AND status IN ('open','escalated_to_human')
		RETURNING id, plan_id::text, conflict_type, discovered_by, discovered_by_role,
			resolved_by, resolved_by_role, resolution, status, resolved_at, created_at`,
		id, resolvedBy, resolvedByRole, resolution)
	return scanConflict(row)
}

func scanConflict(row interface{ Scan(...any) error }) (*Conflict, error) {
	c := &Conflict{}
	return c, row.Scan(&c.ID, &c.PlanID, &c.ConflictType, &c.DiscoveredBy, &c.DiscoveredByRole,
		&c.ResolvedBy, &c.ResolvedByRole, &c.Resolution, &c.Status, &c.ResolvedAt, &c.CreatedAt)
}

// --- Platform status sync ----------------------------------------------------

func (s *Service) SyncPlatformStatus(ctx context.Context, taskID, status string) error {
	_, err := s.Pool.Exec(ctx,
		`UPDATE public.tasks SET platform_status=$2 WHERE id=$1`, taskID, status)
	return err
}
