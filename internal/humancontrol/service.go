// Package humancontrol implements the ReviewDesk surface: the cross-project
// human review queue (Py: human_control). Reads and decisions authenticate
// with the local login session; the decision-taker is always the session
// account, never a client-supplied field.
package humancontrol

import (
	"context"
	"crypto/rand"
	"encoding/json"
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
	ID                 string  `json:"id"`
	ProjectID          string  `json:"project_id"`
	Checkpoint         string  `json:"checkpoint"`
	EvidenceVersion    string  `json:"evidence_version"`
	Title              string  `json:"title"`
	Summary            string  `json:"summary"`
	Status             string  `json:"status"`
	RepositoryID       *string `json:"repository_id"`
	RequestedByAgentID *string `json:"requested_by_agent_id"`
	ResolvedByHumanID  *string `json:"resolved_by_human_id"`
	// Origin / IssueID 来自 request_content：
	//   origin=discovery 表示这条待审**来自 issue 页面的人工步骤**（③ 分档审批 /
	//   ⑤ 物化确认），流水线在那边推进 —— 审核台上要指回 issue，而不是给一个
	//   按了也推不动流水线的按钮。origin=pipeline 才是审核台自己的卡点。
	Origin    string    `json:"origin"`
	IssueID   string    `json:"issue_id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
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

// newID 生成一个 v4 形状的 UUID 字符串（review_requests.id / checkpoint_decisions.id
// 都是 uuid 列，畸形字符串会被 Postgres 直接拒成 22P02）。
//
// 2026-09-19 修：这里此前写的是 `"%x-%x-4%x-8%x-%x"` —— 版本位与变体位**前缀**
// 加在本来就已经是 4 个 hex 字符的段上，于是第三、四段各变成 5 位（全长 34）。
// 它此前从没被调用过（humancontrol 只有读、没有写），所以畸形也一直没暴露；
// 一旦接上生产者（Request/Decide），每一次写入都会 22P02 失败。
// 正确写法与 internal/access/types.go 的 newID 一致：先在字节上打版本/变体位。
func newID() string {
	var buffer [16]byte
	if _, err := rand.Read(buffer[:]); err != nil {
		return ""
	}
	buffer[6] = buffer[6]&15 | 64  // version 4
	buffer[8] = buffer[8]&63 | 128 // variant 10xx
	return fmt.Sprintf("%x-%x-%x-%x-%x", buffer[:4], buffer[4:6], buffer[6:8], buffer[8:10], buffer[10:])
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
		repository_id, requested_by_agent_id::text, decided_by::text, created_at, updated_at,
		COALESCE(request_content->>'origin',''), COALESCE(request_content->>'issue_id','')
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
			&view.RequestedByAgentID, &view.ResolvedByHumanID, &view.CreatedAt, &view.UpdatedAt,
			&view.Origin, &view.IssueID); err != nil {
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

// RequestCommand 建一张人工审核单所需的事实。
type RequestCommand struct {
	ProjectID       string
	Checkpoint      string
	EvidenceVersion string
	Title           string
	Summary         string
	RepositoryID    string
	RequestedBy     string
	// IssueID / Origin 写进 request_content：审核台据此指回 issue（见 ReviewView）。
	IssueID string
	Origin  string
	// Assignee 是"该审的人"。审核台对**非管理员**只显示 assignee = 自己的待审项
	// （见 List），所以发现链落单时必须把它填成项目属主，否则人工卡点在审核台
	// 依然不可见 —— 那正是这次要修的症状。
	Assignee string
}

// Request 建一张人工审核单，**幂等**：同一 (project, checkpoint, evidence_version)
// 的**待审**项只保留一条 —— 发现链会重放，重放不该堆出一排待审。
//
// 2026-09-19 事故：这张表此前**全仓没有任何生产者**（线上实测 0 行、grep 不到
// 任何 INSERT，humancontrol 也只有 List/Decide/Control）。审核台读它，于是从上线
// 起就恒空 —— 用户明明在 issue 里看到人工步骤，审核台却永远是"没有待审事项"。
// 人工卡点必须在这里落单，人工面才有东西可审。
func (s *Service) Request(ctx context.Context, command RequestCommand) (ReviewView, error) {
	if command.ProjectID == "" || command.Checkpoint == "" {
		return ReviewView{}, fmt.Errorf("humancontrol: project and checkpoint are required")
	}
	if existing, err := s.pendingRequest(ctx, command.ProjectID, command.Checkpoint, command.EvidenceVersion); err == nil {
		return existing, nil
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return ReviewView{}, err
	}
	var repositoryID, requestedBy, assignee any
	if command.RepositoryID != "" {
		repositoryID = command.RepositoryID
	}
	if command.RequestedBy != "" {
		requestedBy = command.RequestedBy
	}
	if command.Assignee != "" {
		assignee = command.Assignee
	}
	content, err := json.Marshal(map[string]string{"origin": command.Origin, "issue_id": command.IssueID})
	if err != nil {
		return ReviewView{}, fmt.Errorf("humancontrol: encode request content: %w", err)
	}
	if _, err := s.pool.Exec(ctx, `INSERT INTO public.review_requests
		(id, project_id, object_type, object_id, request_content, status, checkpoint,
		 evidence_version, title, summary, repository_id, requested_by_agent_id, assignee)
		VALUES ($1::uuid, $2::uuid, 'project', $2::uuid, $3::jsonb, 'pending', $4, $5, $6, $7, $8, $9::uuid, $10)`,
		newID(), command.ProjectID, string(content), command.Checkpoint, command.EvidenceVersion,
		command.Title, command.Summary, repositoryID, requestedBy, assignee); err != nil {
		return ReviewView{}, fmt.Errorf("humancontrol: insert request: %w", err)
	}
	return s.pendingRequest(ctx, command.ProjectID, command.Checkpoint, command.EvidenceVersion)
}

// pendingRequest 读回同键的待审项（没有就返回 pgx.ErrNoRows）。
func (s *Service) pendingRequest(ctx context.Context, projectID, checkpoint, evidenceVersion string) (ReviewView, error) {
	rows, err := s.pool.Query(ctx, `SELECT id::text, project_id::text, checkpoint, evidence_version, title, summary, status,
		repository_id, requested_by_agent_id::text, decided_by::text, created_at, updated_at,
		COALESCE(request_content->>'origin',''), COALESCE(request_content->>'issue_id','')
		FROM public.review_requests
		WHERE project_id=$1::uuid AND checkpoint=$2 AND evidence_version=$3 AND status='pending'
		ORDER BY created_at DESC LIMIT 1`, projectID, checkpoint, evidenceVersion)
	if err != nil {
		return ReviewView{}, fmt.Errorf("humancontrol: load pending: %w", err)
	}
	defer rows.Close()
	views, err := scanReviews(rows)
	if err != nil {
		return ReviewView{}, err
	}
	if len(views) == 0 {
		return ReviewView{}, pgx.ErrNoRows
	}
	return views[0], nil
}

// ResolveForCheckpoint 把该项目该类卡点的**待审项**一次性置为已决。
//
// 用途：发现链的人工步骤是在 issue 页面上完成的（③ 分档审批 / ⑤ 物化确认），
// 不是在这里点按钮 —— 所以需要一条"别处已完成"的回写路径，否则审核台会一直挂着
// 已经做完的待审项。
func (s *Service) ResolveForCheckpoint(ctx context.Context, projectID, checkpoint, actor, decision, reason string) error {
	if projectID == "" || checkpoint == "" {
		return nil
	}
	if _, err := s.pool.Exec(ctx, `UPDATE public.review_requests
		SET status=$4, decided_by=NULLIF($3,''), decision_note=$5, decided_at=now(), updated_at=now()
		WHERE project_id=$1::uuid AND checkpoint=$2 AND status='pending'`,
		projectID, checkpoint, actor, decision, reason); err != nil {
		return fmt.Errorf("humancontrol: resolve request: %w", err)
	}
	return nil
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
