// Package discovery implements the issue discovery chain (contract v0.4):
// requirement analysis, candidate scoring, three-tier classification, plan
// generation, tier approval, materialization, over one per-issue state
// document. Without a live model provider the four steps run the
// deterministic keyword path and label themselves llm_used=false (the
// contract honesty clause: keyword fallback must never present itself as
// model scoring).
package discovery

import (
	"context"
	"crypto/sha256"
	"database/sql/driver"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"repomesh.local/repomesh/internal/decisionchain"
	"repomesh.local/repomesh/internal/secrets"
)

// ErrConflict is a 409: a step precondition is not met.
var ErrConflict = errors.New("discovery: precondition not met")

// ErrDrifted is a 409: the approval evidence fingerprint does not match.
var ErrDrifted = errors.New("discovery: evidence version drifted")

// ErrNoRepositories is a 409: the tier decision left nothing to change.
//
// 2026-09-19 事故：候选全部被排除时，系统照样生成了一个 repositories 为空的
// 计划，把错误推迟到"物化确认"才以 500 的样子爆出来（用户只看到"服务端暂时
// 不可用"）。现在在**审批**和**生成计划**两处都拦住，并说清怎么自救。
var ErrNoRepositories = errors.New("discovery: no repositories selected")

// Service reads and advances the discovery chain.
type Service struct {
	pool *pgxpool.Pool

	// decisions is the optional 历史决策 writer: approval and materialize
	// record decision nodes through it, fail-open. Nil = no audit writes.
	decisions *decisionchain.Service

	// secrets 用来解封模型供应商密钥——语义召回要出站调模型。
	// Nil 表示没有出站能力：候选阶段如实回退到关键词路径并标注 llm_used=false。
	secrets *secrets.Store

	// httpClient 可注入（测试用）；nil 时用带超时的默认客户端。
	httpClient *http.Client
}

// New builds the service over the issue schema pool.
func New(pool *pgxpool.Pool) *Service { return &Service{pool: pool} }

// WithSecrets attaches the deployment secret store (composition root).
func (s *Service) WithSecrets(store *secrets.Store) *Service {
	s.secrets = store
	return s
}

// WithHTTPClient overrides the outbound client (tests).
func (s *Service) WithHTTPClient(client *http.Client) *Service {
	s.httpClient = client
	return s
}

// WithDecisions attaches the decision chain writer (composition root).
func (s *Service) WithDecisions(d *decisionchain.Service) *Service {
	s.decisions = d
	return s
}

func newEvidenceVersion(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "|")))
	return hex.EncodeToString(sum[:16])
}

func jsonb(v any) driver.Valuer { return valuer{v} }

type valuer struct{ v any }

func (x valuer) Value() (driver.Value, error) {
	raw, err := json.Marshal(x.v)
	if err != nil {
		return nil, err
	}
	return string(raw), nil
}

// State is the per-issue discovery document (repomesh_issues.issue_discoveries).
type State struct {
	IssueID         string
	ProjectID       string
	RequirementText string
	AnalyzedText    *string
	Analysis        map[string]any
	Candidates      map[string]any
	Classification  map[string]any
	Plan            map[string]any
	Approval        map[string]any
	EvidenceVersion *string
	EffectiveTiers  []any
	Integration     map[string]any
	Materialization map[string]any
	Idempotency     map[string]any
	UpdatedAt       time.Time
}

func nullMap(raw []byte) map[string]any {
	if len(raw) == 0 {
		return nil
	}
	m := map[string]any{}
	if json.Unmarshal(raw, &m) != nil {
		return nil
	}
	return m
}

const stateColumns = "issue_id, project_id, requirement_text, analyzed_requirement, analysis, " +
	"candidates, classification, plan, approval, classification_evidence_version, " +
	"effective_tiers, integration, materialization, idempotency_ledger, updated_at"

func scanState(row pgx.Row) (*State, error) {
	var s State
	var analyzed, evidence *string
	var analysis, candidates, classification, plan, approval, integration, materialization, ledger []byte
	var tiers []byte
	err := row.Scan(&s.IssueID, &s.ProjectID, &s.RequirementText, &analyzed, &analysis,
		&candidates, &classification, &plan, &approval, &evidence,
		&tiers, &integration, &materialization, &ledger, &s.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	s.AnalyzedText = analyzed
	s.Analysis = nullMap(analysis)
	s.Candidates = nullMap(candidates)
	s.Classification = nullMap(classification)
	s.Plan = nullMap(plan)
	s.Approval = nullMap(approval)
	s.EvidenceVersion = evidence
	if len(tiers) > 0 {
		_ = json.Unmarshal(tiers, &s.EffectiveTiers)
	}
	s.Integration = nullMap(integration)
	s.Materialization = nullMap(materialization)
	s.Idempotency = nullMap(ledger)
	return &s, nil
}

func (s *Service) load(ctx context.Context, tx pgx.Tx, issueID string) (*State, error) {
	return scanState(tx.QueryRow(ctx,
		"SELECT "+stateColumns+" FROM repomesh_issues.issue_discoveries WHERE issue_id=$1", issueID))
}

func (s *Service) save(ctx context.Context, tx pgx.Tx, st *State) error {
	tiers, _ := json.Marshal(st.EffectiveTiers)
	query := "INSERT INTO repomesh_issues.issue_discoveries" +
		" (issue_id, project_id, requirement_text, analyzed_requirement, analysis, candidates," +
		" classification, plan, approval, classification_evidence_version, effective_tiers," +
		" integration, materialization, idempotency_ledger, updated_at)" +
		" VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11::jsonb,$12,$13,$14,now())" +
		" ON CONFLICT (issue_id) DO UPDATE SET" +
		" analyzed_requirement=EXCLUDED.analyzed_requirement, analysis=EXCLUDED.analysis," +
		" candidates=EXCLUDED.candidates, classification=EXCLUDED.classification," +
		" plan=EXCLUDED.plan, approval=EXCLUDED.approval," +
		" classification_evidence_version=EXCLUDED.classification_evidence_version," +
		" effective_tiers=EXCLUDED.effective_tiers, integration=EXCLUDED.integration," +
		" materialization=EXCLUDED.materialization, idempotency_ledger=EXCLUDED.idempotency_ledger," +
		" updated_at=now()"
	_, err := tx.Exec(ctx, query,
		st.IssueID, st.ProjectID, st.RequirementText, st.AnalyzedText,
		jsonb(st.Analysis), jsonb(st.Candidates), jsonb(st.Classification), jsonb(st.Plan),
		jsonb(st.Approval), st.EvidenceVersion, string(tiers),
		jsonb(st.Integration), jsonb(st.Materialization), jsonb(st.Idempotency))
	return err
}

// ensureIssue loads the issue (title and description feed requirement_text)
// and returns pgx.ErrNoRows when the issue does not exist.
func (s *Service) ensureIssue(ctx context.Context, tx pgx.Tx, issueID string) (string, string, error) {
	var title, description string
	err := tx.QueryRow(ctx,
		"SELECT title, description FROM repomesh_issues.issues WHERE id=$1", issueID).
		Scan(&title, &description)
	return title, description, err
}

// replay checks the idempotency ledger; a repeated key returns the original
// receipt (status=replayed) instead of re-running the step.
func replay(st *State, key string) (map[string]any, bool) {
	if key == "" {
		return nil, false
	}
	if raw, ok := st.Idempotency[key]; ok {
		if receipt, ok := raw.(map[string]any); ok {
			return receipt, true
		}
	}
	return nil, false
}

func recordReceipt(st *State, key string, receipt map[string]any) {
	if key == "" {
		return
	}
	if st.Idempotency == nil {
		st.Idempotency = map[string]any{}
	}
	st.Idempotency[key] = receipt
}

// View is the GET /issues/{id}/discovery read projection (contract 3.1).
func (st *State) View() map[string]any {
	step, state, running := deriveStep(st)
	view := map[string]any{
		"issue_id":                        st.IssueID,
		"plan_version":                    1,
		"step":                            step,
		"step_state":                      state,
		"running_task_id":                 running,
		"requirement_text":                st.RequirementText,
		"analyzed_requirement":            st.AnalyzedText,
		"analysis":                        st.Analysis,
		"candidates":                      st.Candidates,
		"classification":                  st.Classification,
		"classification_evidence_version": st.EvidenceVersion,
		"effective_tiers":                 st.EffectiveTiers,
		"approval":                        st.Approval,
		"integration":                     st.Integration,
		"materialization":                 st.Materialization,
	}
	if view["approval"] == nil {
		view["approval"] = map[string]any{"state": "not_requested", "evidence_version": nil,
			"decided_by_agent_id": nil, "reason": "", "decided_at": nil}
	}
	return view
}

// deriveStep implements the contract 3.2 order: the first missing block wins.
func deriveStep(st *State) (int, string, *string) {
	if st.Materialization != nil {
		if status, _ := st.Materialization["status"].(string); status == "failed" {
			return 4, "failed", nil
		}
		return 4, "done", nil
	}
	if st.Plan != nil {
		return 4, "done", nil
	}
	if st.Approval != nil {
		if s, _ := st.Approval["state"].(string); s == "approved" {
			return 4, "idle", nil
		}
	}
	if st.Classification != nil {
		if e, _ := st.Classification["error"].(map[string]any); e != nil {
			return 3, "failed", nil
		}
		return 3, "done", nil
	}
	if st.Candidates != nil {
		if e, _ := st.Candidates["error"].(map[string]any); e != nil {
			return 2, "failed", nil
		}
		return 2, "done", nil
	}
	if st.Analysis != nil {
		if e, _ := st.Analysis["error"].(map[string]any); e != nil {
			return 1, "failed", nil
		}
		return 1, "done", nil
	}
	return 1, "idle", nil
}

// Answer is one clarification Q&A pair appended to the requirement text.
type Answer struct {
	Question string `json:"question"`
	Answer   string `json:"answer"`
}
