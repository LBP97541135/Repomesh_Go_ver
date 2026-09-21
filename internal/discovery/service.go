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
	"repomesh.local/repomesh/internal/observability"
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
	modelObserver func(observability.ModelCall)

	pool *pgxpool.Pool

	// decisions is the optional 历史决策 writer: approval and materialize
	// record decision nodes through it, fail-open. Nil = no audit writes.
	decisions *decisionchain.Service

	// secrets 用来解封模型供应商密钥——语义召回要出站调模型。
	// Nil 表示没有出站能力：候选阶段如实回退到关键词路径并标注 llm_used=false。
	secrets *secrets.Store

	// httpClient 可注入（测试用）；nil 时用带超时的默认客户端。
	httpClient *http.Client

	// replanner 是重排 v2 的落库端口（组合根接 tasks.PostgresStore.Replan）。
	// Nil = 未接线：第 6 步（重排）会**如实失败**，而不是报一个"已重排"的假成功。
	replanner PlanReplanner
}

// New builds the service over the issue schema pool.
func New(pool *pgxpool.Pool) *Service { return &Service{pool: pool} }

// WithSecrets attaches the deployment secret store (composition root).
func (s *Service) WithSecrets(store *secrets.Store) *Service {
	s.secrets = store
	return s
}

func (s *Service) WithModelObserver(observer func(observability.ModelCall)) *Service {
	s.modelObserver = observer
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

// WithReplanner attaches the v2 replan port (composition root).
func (s *Service) WithReplanner(r PlanReplanner) *Service {
	s.replanner = r
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
	// RunningStep / RunningRunID 是**在途的规划步**（0 / nil = 没有在途）。
	//
	// 2026-09-20：读面此前**从来不报"进行中"** —— `deriveStep` 的三个返回值里
	// 第三个恒为 nil，于是 `running_task_id` 永远是 null、`step_state` 也永远
	// 不等于 "running"。后果是界面上「进行中」这个状态根本显示不出来：agent
	// 真的在跑（planning_runs 有行、agent_runs 有 run），用户看到的却是"待开始"
	// 或"等待前序"，只能干等 —— 这是「看不到进度」「没有实时 DAG」的共同上游。
	//
	// 事实来源是 `repomesh_issues.planning_runs`：`state='pending'` 就是在途
	// （协调器派发时只写 run_id，不改 state；跑完才改 succeeded/failed）。
	RunningStep  int
	RunningRunID *string
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

// ensureState 取发现链状态；**没有就按 issue 现场补一份**。
//
// 2026-09-20 线上实测：issue 刚建好，自动托管就把 ① 派给了 agent，产物回来写库时
// 才发现 issue_discoveries 里还没有这一行（那一行此前只有 UI 走 steps.go 分析时才
// 建），整步于是以「还没有发现链状态」失败 —— agent 明明跑成功（exit 0），界面却
// 什么都不显示。状态是**派发的前置条件，不是派发的结果**：这里按 issue 的标题+描述
// 补一份，字段照实取自 issue 行，不编造内容。
func (s *Service) ensureState(ctx context.Context, tx pgx.Tx, issueID string) (*State, error) {
	st, err := s.load(ctx, tx, issueID)
	if err != nil {
		return nil, err
	}
	if st != nil {
		return st, nil
	}
	var title, description, projectID string
	if err := tx.QueryRow(ctx,
		"SELECT title, description, project_id FROM repomesh_issues.issues WHERE id=$1", issueID).
		Scan(&title, &description, &projectID); err != nil {
		return nil, err
	}
	return &State{
		IssueID:         issueID,
		ProjectID:       projectID,
		RequirementText: strings.TrimSpace(title + "\n" + description),
	}, nil
}

func (s *Service) save(ctx context.Context, tx pgx.Tx, st *State) error {
	// Serialize the history boundary using the canonical Issue identity. This
	// is the same lock order as archive/purge: Issue before discovery/history.
	// NO KEY UPDATE is compatible with the FK KEY SHARE locks that Plan and
	// Materialize may already hold; upgrading those to UPDATE can deadlock.
	var issueID string
	if err := tx.QueryRow(ctx, `SELECT id FROM repomesh_issues.issues
		WHERE project_id=$1 AND id=$2 FOR NO KEY UPDATE`, st.ProjectID, st.IssueID).Scan(&issueID); err != nil {
		return err
	}
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
	if err != nil {
		return err
	}
	// Read back what was actually saved: ON CONFLICT deliberately preserves
	// some original fields, and JSONB normalizes values on the business row.
	// Receipt-ledger-only changes do not become new decision facts.
	var snapshot json.RawMessage
	err = tx.QueryRow(ctx, `SELECT jsonb_build_object(
		'schema_version',$2::text,'event_kind','discovery.state_saved',
		'issue_id',issue_id,'project_id',project_id,
		'requirement_text',requirement_text,'analyzed_requirement',analyzed_requirement,
		'analysis',analysis,'candidates',candidates,'classification',classification,
		'plan',plan,'approval',approval,'classification_evidence_version',classification_evidence_version,
		'effective_tiers',effective_tiers,'integration',integration,'materialization',materialization,
		'scope_gate',scope_gate)
		FROM repomesh_issues.issue_discoveries WHERE issue_id=$1`, st.IssueID, observability.DiscoverySourceVersion).Scan(&snapshot)
	if err != nil {
		return err
	}
	return observability.AppendDiscoveryFact(ctx, tx, st.ProjectID, st.IssueID, snapshot)
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
	step, state, _ := deriveStep(st)
	// `running_task_id` 此前恒为 nil（deriveStep 的第三个返回值永远是 nil），
	// 于是前端"有东西在跑"的判断（`running_task_id !== null`）永远为假 ——
	// 界面上「进行中」显示不出来。现在如实取自 planning_runs（见 fillRunning）。
	// `running_step` 是新增的显式字段：让前端能按**产物缺哪一步 + 哪一步在途**
	// 推导状态，而不是去猜 `step`/`step_state` 的语义（那两者表示的是
	// "已完成到哪一步"，历史上已经被误用成"下一步该做什么"，见 treeModel.ts）。
	running := st.RunningRunID
	runningStep := 0
	if st.RunningStep > 0 {
		runningStep = st.RunningStep
		if state != "failed" {
			state = "running"
		}
	}
	view := map[string]any{
		"issue_id":                        st.IssueID,
		"plan_version":                    1,
		"step":                            step,
		"step_state":                      state,
		"running_task_id":                 running,
		"running_step":                    runningStep,
		"requirement_text":                st.RequirementText,
		"analyzed_requirement":            st.AnalyzedText,
		"analysis":                        st.Analysis,
		"candidates":                      st.Candidates,
		"classification":                  st.Classification,
		"classification_evidence_version": st.EvidenceVersion,
		"effective_tiers":                 st.EffectiveTiers,
		"approval":                        st.Approval,
		// plan 块（④ 生成计划的产出；未生成 → null）。此前读面不输出它，
		// 前端阶段历史与「④ 已生成」判定全部拿不到事实（2026-09-20 补）。
		"plan":            st.Plan,
		"integration":     st.Integration,
		"materialization": st.Materialization,
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
