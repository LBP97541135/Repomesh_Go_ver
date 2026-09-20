package decisionchain

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresStore implements Store on the shared pool. Tables and columns are
// owned by migration 0008 (改动清单); the shapes there are authoritative.
type PostgresStore struct {
	pool *pgxpool.Pool
}

// NewPostgresStore builds the store (composition root only).
func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore { return &PostgresStore{pool: pool} }

// nodeColumns selects the node columns through the alias n: the JOIN queries
// against decision_embeddings share the id column name, and unqualified
// columns are ambiguous there (审查 C1).
const nodeColumns = `n.id, n.event_id, n.requirement_text, n.requirement_key,
  COALESCE(n.project_id::text, ''), COALESCE(n.parent_node_id::text, ''),
  n.step, n.version, n.status, n.actor_type, n.actor_id, n.action, n.rationale,
  n.context_ref, n.affected_repositories, n.source, n.created_at`

func scanNode(row pgx.Row, extra ...any) (DecisionNode, error) {
	var (
		n                 DecisionNode
		eventID           *string
		contextRef, repos []byte
	)
	dest := []any{&n.ID, &eventID, &n.RequirementText, &n.RequirementKey,
		&n.ProjectID, &n.ParentNodeID, &n.Step, &n.Version, &n.Status,
		&n.ActorType, &n.ActorID, &n.Action, &n.Rationale,
		&contextRef, &repos, &n.Source, &n.CreatedAt}
	if err := row.Scan(append(dest, extra...)...); err != nil {
		return DecisionNode{}, err
	}
	if eventID != nil {
		n.EventID = *eventID
	}
	n.ContextRef = map[string]any{}
	if len(contextRef) > 0 {
		if err := json.Unmarshal(contextRef, &n.ContextRef); err != nil {
			return DecisionNode{}, fmt.Errorf("decode context_ref: %w", err)
		}
	}
	n.AffectedRepositories = []string{}
	if len(repos) > 0 {
		if err := json.Unmarshal(repos, &n.AffectedRepositories); err != nil {
			return DecisionNode{}, fmt.Errorf("decode affected_repositories: %w", err)
		}
	}
	return n, nil
}

// RecordPlanRevisedInTx 双轴挂钩（协议 §2.4）：在计划换代的事务里落一条
// 决策单（status=adjusted，upstream_ref 指 BLOCKED 节点）——计划轴与决策轴
// 同事务提交，要么同时生效要么同时回滚。Event 需自带 Step/Status/UpstreamRef。
func (p *PostgresStore) RecordPlanRevisedInTx(ctx context.Context, tx pgx.Tx, e Event) (DecisionNode, error) {
	w, err := buildNodeWrite(ctx, e, nil)
	if err != nil {
		return DecisionNode{}, err
	}
	return p.recordTx(ctx, tx, w)
}

// Record inserts one node; idempotent by event_id. Version assignment is
// serialized per (requirement_key, step) by a transaction-scoped advisory
// lock, so the partial unique index stays a pure backstop (F9 修正：重试
// 换加锁，重试在并发下不保证成功，加锁才是根因解).
func (p *PostgresStore) Record(ctx context.Context, w nodeWrite) (DecisionNode, error) {
	return p.recordOnce(ctx, w)
}

func (p *PostgresStore) recordOnce(ctx context.Context, w nodeWrite) (DecisionNode, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return DecisionNode{}, err
	}
	defer tx.Rollback(ctx)
	stored, err := p.recordTx(ctx, tx, w)
	if err != nil {
		return DecisionNode{}, err
	}
	return stored, tx.Commit(ctx)
}

func (p *PostgresStore) recordTx(ctx context.Context, tx pgx.Tx, w nodeWrite) (DecisionNode, error) {
	// Serialize same-chain writers so max+1 cannot race.
	if _, err := tx.Exec(ctx,
		`SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`,
		w.RequirementKey+":"+string(w.Step)); err != nil {
		return DecisionNode{}, err
	}

	var version int
	if err := tx.QueryRow(ctx,
		`SELECT COALESCE(MAX(version), 0) + 1 FROM public.decision_chain_nodes
		 WHERE requirement_key = $1 AND step = $2`, w.RequirementKey, string(w.Step),
	).Scan(&version); err != nil {
		return DecisionNode{}, err
	}

	contextRef, err := json.Marshal(w.ContextRef)
	if err != nil {
		return DecisionNode{}, fmt.Errorf("encode context_ref: %w", err)
	}
	if contextRef == nil {
		contextRef = []byte("{}")
	}
	repos, err := json.Marshal(w.AffectedRepositories)
	if err != nil {
		return DecisionNode{}, fmt.Errorf("encode affected_repositories: %w", err)
	}
	if repos == nil {
		repos = []byte("[]")
	}

	id := NewUUIDv4()
	tag, err := tx.Exec(ctx, `
		INSERT INTO public.decision_chain_nodes (
		  id, event_id, requirement_text, requirement_key, actor_type, actor_id,
		  parent_node_id, action, rationale, context_ref, step, version, status,
		  affected_repositories, source, project_id
		) VALUES ($1, $2, $3, $4, COALESCE(NULLIF($5, ''), 'human'), $6, NULLIF($7, '')::uuid, $8, $9, $10, $11, $12, $13, $14, $15, NULLIF($16, '')::uuid)
		ON CONFLICT (event_id) WHERE event_id IS NOT NULL DO NOTHING`,
		id, w.EventID, w.RequirementText, w.RequirementKey, w.ActorType, w.ActorID,
		w.ParentNodeID, w.Action, w.Rationale, contextRef, string(w.Step), version,
		string(w.Status), repos, string(w.Source), w.ProjectID)
	if err != nil {
		return DecisionNode{}, err
	}

	var stored DecisionNode
	if tag.RowsAffected() == 0 {
		// Idempotent redelivery: return the already-stored node unchanged.
		stored, err = p.byEventID(ctx, tx, w.EventID)
	} else {
		stored, err = p.byID(ctx, tx, id)
	}
	if err != nil {
		return DecisionNode{}, err
	}
	return stored, nil
}

func (p *PostgresStore) byEventID(ctx context.Context, tx pgx.Tx, eventID string) (DecisionNode, error) {
	row := tx.QueryRow(ctx,
		`SELECT `+nodeColumns+` FROM public.decision_chain_nodes n WHERE n.event_id = $1 LIMIT 1`, eventID)
	return scanNode(row)
}

func (p *PostgresStore) byID(ctx context.Context, tx pgx.Tx, id string) (DecisionNode, error) {
	row := tx.QueryRow(ctx,
		`SELECT `+nodeColumns+` FROM public.decision_chain_nodes n WHERE n.id = $1`, id)
	return scanNode(row)
}

// Get reads one node by id; pgx.ErrNoRows when absent.
func (p *PostgresStore) Get(ctx context.Context, id string) (*DecisionNode, error) {
	row := p.pool.QueryRow(ctx,
		`SELECT `+nodeColumns+` FROM public.decision_chain_nodes n WHERE n.id = $1`, id)
	node, err := scanNode(row)
	if err != nil {
		return nil, err
	}
	return &node, nil
}

// List applies the filter; newest first.
//
// 隔离（2026-09-20）：f.Scope.constrained() 时只返回调用者自己项目下的节点
// （JOIN repomesh_projects.projects 按 owner 过滤）。当前阶段所有账号同等对待，
// 无管理员放权；零值 Scope 不加约束（仅内部/测试）。
func (p *PostgresStore) List(ctx context.Context, f Filter) ([]DecisionNode, error) {
	where := []string{"true"}
	join := ""
	args := []any{}
	if f.Scope.constrained() {
		join = ` JOIN repomesh_projects.projects p ON p.id = n.project_id::text`
		args = append(args, f.Scope.ActorID)
		where = append(where, fmt.Sprintf("p.owner = $%d", len(args)))
	}
	if f.RequirementKey != "" {
		args = append(args, f.RequirementKey)
		where = append(where, fmt.Sprintf("n.requirement_key = $%d", len(args)))
	}
	if f.Step != nil && *f.Step != "" {
		args = append(args, string(*f.Step))
		where = append(where, fmt.Sprintf("n.step = $%d", len(args)))
	}
	if f.ProjectID != "" {
		args = append(args, f.ProjectID)
		where = append(where, fmt.Sprintf("n.project_id = $%d::uuid", len(args)))
	}
	if f.Repository != "" {
		args = append(args, f.Repository)
		where = append(where, fmt.Sprintf("n.affected_repositories ? $%d", len(args)))
	}
	if f.Keyword != "" {
		args = append(args, "%"+f.Keyword+"%")
		where = append(where, fmt.Sprintf("n.requirement_text ILIKE $%d", len(args)))
	}
	limit := f.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	args = append(args, limit, f.Offset)
	rows, err := p.pool.Query(ctx, `SELECT `+nodeColumns+` FROM public.decision_chain_nodes n`+join+`
		WHERE `+strings.Join(where, " AND ")+`
		ORDER BY n.created_at DESC, n.id DESC LIMIT $`+strconv.Itoa(len(args)-1)+` OFFSET $`+strconv.Itoa(len(args)),
		args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	nodes := []DecisionNode{}
	for rows.Next() {
		node, err := scanNode(rows)
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, node)
	}
	return nodes, rows.Err()
}

// PendingEmbeddings returns nodes whose embedding row is missing or was made
// with a different model (F5 model-mix guard).
func (p *PostgresStore) PendingEmbeddings(ctx context.Context, model string, limit int) ([]DecisionNode, error) {
	if limit <= 0 {
		return nil, nil
	}
	rows, err := p.pool.Query(ctx, `SELECT `+nodeColumns+`
		FROM public.decision_chain_nodes n
		LEFT JOIN public.decision_embeddings e ON e.node_id = n.id
		WHERE e.node_id IS NULL OR e.model <> $1
		ORDER BY n.created_at
		LIMIT $2`, model, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	nodes := []DecisionNode{}
	for rows.Next() {
		node, err := scanNode(rows)
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, node)
	}
	return nodes, rows.Err()
}

// UpsertEmbedding writes the vector row: primary vector column and JSON
// fallback copy in one statement (D11 double write).
func (p *PostgresStore) UpsertEmbedding(ctx context.Context, nodeID, model string, vec []float32, embeddedAt time.Time) error {
	encoded, err := json.Marshal(vec)
	if err != nil {
		return fmt.Errorf("encode embedding: %w", err)
	}
	tag, err := p.pool.Exec(ctx, `
		INSERT INTO public.decision_embeddings (id, node_id, embedding, model, embedding_vec, embedded_at)
		VALUES (gen_random_uuid(), $1, $2::jsonb, $3, $4::public.vector, $5)
		ON CONFLICT (node_id) DO UPDATE SET
		  embedding = EXCLUDED.embedding,
		  model = EXCLUDED.model,
		  embedding_vec = EXCLUDED.embedding_vec,
		  embedded_at = EXCLUDED.embedded_at`,
		nodeID, encoded, model, formatVector(vec), embeddedAt)
	_ = tag
	return err
}

// SemanticCandidates orders current-model embeddings by cosine distance to
// the query vector (pgvector HNSW path); score = 1 - distance. scope confines
// hits to the caller's own projects (zero Scope = unconstrained).
func (p *PostgresStore) SemanticCandidates(ctx context.Context, model string, query []float32, limit int, scope Scope) ([]ScoredNode, error) {
	if limit <= 0 {
		return nil, nil
	}
	join := ""
	args := []any{model, formatVector(query)}
	where := `e.model = $1 AND e.embedding_vec IS NOT NULL`
	if scope.constrained() {
		join = ` JOIN repomesh_projects.projects p ON p.id = n.project_id::text`
		args = append(args, scope.ActorID)
		where += fmt.Sprintf(" AND p.owner = $%d", len(args))
	}
	args = append(args, limit)
	rows, err := p.pool.Query(ctx, `SELECT `+nodeColumns+`,
		  1 - (e.embedding_vec OPERATOR(public.<=>) $2::public.vector) AS score
		FROM public.decision_chain_nodes n
		JOIN public.decision_embeddings e ON e.node_id = n.id`+join+`
		WHERE `+where+`
		ORDER BY e.embedding_vec OPERATOR(public.<=>) $2::public.vector
		LIMIT $`+strconv.Itoa(len(args)), args...)
	if err != nil {
		return nil, err
	}
	return collectScored(rows)
}

// StructuralCandidates prescreens nodes sharing any of the given repository
// names via the GIN index; scoring (Jaccard) is the caller's job. scope 同 SemanticCandidates。
func (p *PostgresStore) StructuralCandidates(ctx context.Context, names []string, limit int, scope Scope) ([]DecisionNode, error) {
	if len(names) == 0 {
		return nil, nil
	}
	join := ""
	args := []any{names}
	where := `n.affected_repositories ?| $1::text[]`
	if scope.constrained() {
		join = ` JOIN repomesh_projects.projects p ON p.id = n.project_id::text`
		args = append(args, scope.ActorID)
		where += fmt.Sprintf(" AND p.owner = $%d", len(args))
	}
	args = append(args, limit)
	rows, err := p.pool.Query(ctx, `SELECT `+nodeColumns+`
		FROM public.decision_chain_nodes n`+join+`
		WHERE `+where+`
		ORDER BY n.created_at DESC
		LIMIT $`+strconv.Itoa(len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	nodes := []DecisionNode{}
	for rows.Next() {
		node, err := scanNode(rows)
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, node)
	}
	return nodes, rows.Err()
}

// FeatureEnabled returns the toggle; a missing row counts as enabled (D12).
func (p *PostgresStore) FeatureEnabled(ctx context.Context, feature string) (bool, error) {
	var enabled bool
	err := p.pool.QueryRow(ctx,
		`SELECT enabled FROM public.feature_settings WHERE feature = $1`, feature).Scan(&enabled)
	if errors.Is(err, pgx.ErrNoRows) {
		return true, nil
	}
	return enabled, err
}

// SetFeature writes the toggle with its audit fields.
func (p *PostgresStore) SetFeature(ctx context.Context, feature string, enabled bool, updatedBy string, at time.Time) error {
	tag, err := p.pool.Exec(ctx, `
		INSERT INTO public.feature_settings (feature, enabled, updated_by, updated_at)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (feature) DO UPDATE SET
		  enabled = EXCLUDED.enabled,
		  updated_by = EXCLUDED.updated_by,
		  updated_at = EXCLUDED.updated_at`,
		feature, enabled, updatedBy, at)
	_ = tag
	return err
}

func collectScored(rows pgx.Rows) ([]ScoredNode, error) {
	defer rows.Close()
	hits := []ScoredNode{}
	for rows.Next() {
		var score float64
		node, err := scanNode(rows, &score)
		if err != nil {
			return nil, err
		}
		hits = append(hits, ScoredNode{Node: node, Score: score})
	}
	return hits, rows.Err()
}

// formatVector renders a pgvector text literal: [1,2,3].
func formatVector(vec []float32) string {
	if len(vec) == 0 {
		return "[]"
	}
	buf := make([]byte, 0, len(vec)*8+2)
	buf = append(buf, '[')
	for i, v := range vec {
		if i > 0 {
			buf = append(buf, ',')
		}
		buf = strconv.AppendFloat(buf, float64(v), 'g', -1, 32)
	}
	return string(append(buf, ']'))
}

// RecordFeedback records one escalation-ladder hop (blocked report, TM
// escalation, or TM close) in its own transaction and returns the node id.
func (p *PostgresStore) RecordFeedback(ctx context.Context, e Event) (DecisionNode, error) {
	w, err := buildNodeWrite(ctx, e, nil)
	if err != nil {
		return DecisionNode{}, err
	}
	return p.recordOnce(ctx, w)
}

// BlockedSince lists blocked feedback nodes for a requirement since the
// given instant (collection window query, 协议 §2 步骤 3).
func (p *PostgresStore) BlockedSince(ctx context.Context, requirementKey string, since time.Time) ([]DecisionNode, error) {
	rows, err := p.pool.Query(ctx, `SELECT `+nodeColumns+`
		FROM public.decision_chain_nodes n
		WHERE n.requirement_key = $1 AND n.status = 'blocked' AND n.created_at >= $2
		ORDER BY n.created_at`, requirementKey, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DecisionNode{}
	for rows.Next() {
		node, err := scanNode(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, node)
	}
	return out, rows.Err()
}
