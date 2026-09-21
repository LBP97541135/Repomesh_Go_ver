// 选仓门(spec 2026-09-20 §2/§3.2):建 issue 不再选仓,① 需求分析后出现
// 「我自己勾 / 让 AI 定」的门,确认的集合就是本 issue 的仓库范围。
//
// 门状态放在 issue_discoveries 的**独立 jsonb 列** scope_gate 上,而不是塞进
// 现有块(candidates 等):旧二进制的 save() 整行重写会无声抹掉子键
// (reopen.go 等整块赋值路径同理)。因此这里所有写入都只动 scope_gate 这一列
// (开门是"填空列",确认是带 WHERE state='pending' 的 CAS UPDATE),
// **绝不走 discovery.save()**,避免与发现链的整行写互相丢更新。
package discovery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// GateState 是门的两个稳态:pending=等人/AI 选;resolved=范围已确认。
type GateState string

const (
	GatePending  GateState = "pending"
	GateResolved GateState = "resolved"
)

// Gate 是 scope_gate 列的读面。空列(老 issue)读 nil,调用方视为已
// resolve、直接跳过门——老 issue 不回填。
type Gate struct {
	State     GateState
	DecidedBy string // manual|ai|timeout
	// Suggested 是建议的**仓名列表**（历史读法：options 采纳、超时代选都按名字映射 id）。
	// 完整形状（分数/档位/理由）见 Suggestions。
	Suggested   []string
	Suggestions []GateSuggestion
	DeadlineAt  *time.Time // 只在 ai 模式置(开门+10 分钟);hitl 无截止、门无限等待
	ResolvedAt  *time.Time
	// AIRequested 记「人在门上点了『让 AI 定』」(spec 2026-09-20 修订):门先出现,
	// 点了才去生成建议。ai 语义的门 10 分钟无人点由协调器走同一条生成+采纳。
	AIRequested bool
}

// GateSuggestion 是选仓门建议列表里的一行（2026-09-21 用户裁定）：仓名 + 置信度
// 分数（0..1）+ 档位标签 + 理由。理由默认折叠由前端决定，后端只如实存下四样。
// **排除档不进这份列表**：门只发「建议纳入的仓」，把 agent 判过排除的仓又摆回人
// 面前等于噪音。
type GateSuggestion struct {
	Repository string  `json:"repository"`
	Score      float64 `json:"score"`
	Tier       string  `json:"tier"` // required|maybe
	Reason     string  `json:"reason"`
}

// suggestedJSON 兼容两种落库形状：老数据是仓名数组（`[]string`），2026-09-21 起
// 是带分数/档位/理由的对象数组。读面**永远折成对象数组**——仓名字符串折成只有
// repository 的条目，前端因此只需处理一种形状。
type suggestedJSON []GateSuggestion

func (s *suggestedJSON) UnmarshalJSON(raw []byte) error {
	// 先按老形状（仓名数组）解；解不动再按新形状（对象数组）解。
	var names []string
	if err := json.Unmarshal(raw, &names); err == nil {
		converted := make(suggestedJSON, 0, len(names))
		for _, name := range names {
			converted = append(converted, GateSuggestion{Repository: name})
		}
		*s = converted
		return nil
	}
	var objects []GateSuggestion
	if err := json.Unmarshal(raw, &objects); err != nil {
		return err
	}
	*s = objects
	return nil
}

func (s suggestedJSON) MarshalJSON() ([]byte, error) {
	if s == nil {
		return []byte("[]"), nil
	}
	return json.Marshal([]GateSuggestion(s))
}

// names 只取仓名（历史读法：options 采纳、超时代选都按名字映射项目内 id）。
func (s suggestedJSON) names() []string {
	names := make([]string, 0, len(s))
	for _, item := range s {
		if name := strings.TrimSpace(item.Repository); name != "" {
			names = append(names, name)
		}
	}
	return names
}

// suggestionsFromNames 把仓名折成建议条目（只有 repository 的薄形状）。
func suggestionsFromNames(names []string) suggestedJSON {
	converted := make(suggestedJSON, 0, len(names))
	for _, name := range names {
		if trimmed := strings.TrimSpace(name); trimmed != "" {
			converted = append(converted, GateSuggestion{Repository: trimmed})
		}
	}
	return converted
}

// gateJSON 与列内 JSON 形状一一对应;时间用 RFC3339 文本存取。
type gateJSON struct {
	State       string        `json:"state"`
	DecidedBy   string        `json:"decided_by"`
	Suggested   suggestedJSON `json:"suggested"`
	DeadlineAt  *time.Time    `json:"deadline_at"`
	ResolvedAt  *time.Time    `json:"resolved_at"`
	AIRequested bool          `json:"ai_requested"`
}

// gateAutoDeadline 是 ai 模式选仓门的自动代选宽限(spec 2026-09-20 §1):
// 开门时刻 + 10 分钟无人确认,协调器按建议集合代选。
const gateAutoDeadline = 10 * time.Minute

// OpenGate 打开选仓门:issue_discoveries 没有 scope_gate(行缺或列为空)才写,
// 已有门(pending 或 resolved)一律不动——重复开门幂等且不覆盖。发现链行
// 缺失时按 issue 现场补一行最小状态(与 ensureState 同思路),门不依赖
// "① 已经跑过"。
func (s *Service) OpenGate(ctx context.Context, issueID string, suggested []string, deadline *time.Time) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := openGateInTx(ctx, tx, issueID, suggested, deadline); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// openGateInTx 是同事务版:② 候选产物落库的事务里顺手开门(gate_flow),
// 候选与门要么一起提交、要么一起回滚;单列写不受 save() 整行重写影响。
func openGateInTx(ctx context.Context, tx pgx.Tx, issueID string, suggested []string, deadline *time.Time) error {
	payload, err := json.Marshal(gateJSON{State: string(GatePending), Suggested: suggestionsFromNames(suggested), DeadlineAt: deadline})
	if err != nil {
		return err
	}
	command, err := tx.Exec(ctx, `INSERT INTO repomesh_issues.issue_discoveries
		(issue_id, project_id, requirement_text, scope_gate)
		SELECT i.id, i.project_id, btrim(i.title || E'\n' || i.description), $2::jsonb
		FROM repomesh_issues.issues i
		WHERE i.id = $1
		ON CONFLICT (issue_id) DO UPDATE SET scope_gate = EXCLUDED.scope_gate, updated_at = now()
		WHERE issue_discoveries.scope_gate IS NULL`, issueID, string(payload))
	if err != nil {
		return err
	}
	if command.RowsAffected() == 0 {
		// 0 行要么是门已存在(幂等,正常),要么是 issue 本身不存在——后者要
		// 如实拒绝,不能静默假装开了门。
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM repomesh_issues.issues WHERE id=$1)`, issueID).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("%w: 开门失败,issue 不存在: %s", ErrConflict, issueID)
		}
	}
	return nil
}

// ResolveGate 把 pending 的门翻成 resolved(单列 CAS UPDATE,不走 save())。
// 返回是否真的翻转:门不存在、已 resolved 的都返回 false,由调用方决定语义。
func (s *Service) ResolveGate(ctx context.Context, issueID, decidedBy string) (bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	flipped, err := ResolveGateInTx(ctx, tx, issueID, decidedBy)
	if err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return flipped, nil
}

// ResolveGateInTx 是同事务版:批量确认端点(web)要在写范围双表的**同一个**
// 事务里把门置 resolved,整组提交或整组回滚。确认/超时都保留 suggested 与
// deadline 作为历史事实,只补决定人与决定时刻。
func ResolveGateInTx(ctx context.Context, tx pgx.Tx, issueID, decidedBy string) (bool, error) {
	command, err := tx.Exec(ctx, `UPDATE repomesh_issues.issue_discoveries
		SET scope_gate = jsonb_build_object(
			'state', 'resolved',
			'decided_by', $2::text,
			'suggested', COALESCE(scope_gate -> 'suggested', '[]'::jsonb),
			'deadline_at', scope_gate -> 'deadline_at',
			'ai_requested', COALESCE(scope_gate -> 'ai_requested', 'false'::jsonb),
			'resolved_at', to_jsonb(now())),
			updated_at = now()
		WHERE issue_id = $1 AND scope_gate ->> 'state' = 'pending'`, issueID, decidedBy)
	if err != nil {
		return false, err
	}
	return command.RowsAffected() == 1, nil
}

// Gate 读门;无行或列空(老 issue)返回 nil。
func (s *Service) Gate(ctx context.Context, issueID string) (*Gate, error) {
	return GateInTx(ctx, s.pool, issueID)
}

// GateInTx 是同事务版读门:批量确认端点(A3)在写范围内的事务里先读建议集合,
// 与后面的双表写共享同一快照。
func GateInTx(ctx context.Context, q interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}, issueID string) (*Gate, error) {
	var raw []byte
	err := q.QueryRow(ctx,
		`SELECT scope_gate FROM repomesh_issues.issue_discoveries WHERE issue_id=$1`, issueID).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 {
		return nil, nil
	}
	var payload gateJSON
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, err
	}
	gate := &Gate{
		State:       GateState(payload.State),
		DecidedBy:   payload.DecidedBy,
		Suggested:   payload.Suggested.names(),
		Suggestions: payload.Suggested,
		DeadlineAt:  payload.DeadlineAt,
		ResolvedAt:  payload.ResolvedAt,
		AIRequested: payload.AIRequested,
	}
	if gate.Suggested == nil {
		gate.Suggested = []string{}
	}
	if gate.Suggestions == nil {
		gate.Suggestions = []GateSuggestion{}
	}
	return gate, nil
}

// FillGateSuggested 补建议(单列 CAS UPDATE,绝不走 save()):**只在门还 pending
// 时写**——已决(resolved)的门不复活、不被改写。① 分析应用即开门(建议为空),
// ② 候选产物落地时才把候选全名补进来;门不是因为候选落库才出现/消失。
//
// 这是**接受仓名的薄包装**（历史调用方与测试用）：把仓名折成只有 repository 的
// 建议条目。带分数/档位/理由的完整形状走 fillGateSuggestedForCandidates。
func (s *Service) FillGateSuggested(ctx context.Context, issueID string, names []string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := fillGateSuggestedInTx(ctx, tx, issueID, suggestionsFromNames(names)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// fillGateSuggestedInTx 是同事务版:② 候选产物落库的事务里顺手补建议,与候选块
// 一起提交/回滚。建议以**对象数组**落库（repository/score/tier/reason）。
func fillGateSuggestedInTx(ctx context.Context, tx pgx.Tx, issueID string, suggestions suggestedJSON) error {
	payload, err := json.Marshal(suggestions)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE repomesh_issues.issue_discoveries
		SET scope_gate = jsonb_set(scope_gate, '{suggested}', $2::jsonb, true), updated_at = now()
		WHERE issue_id=$1 AND scope_gate ->> 'state' = 'pending'`, issueID, string(payload)); err != nil {
		return fmt.Errorf("discovery: 补选仓门建议: %w", err)
	}
	return nil
}

// MarkGateAIRequested 记「人点了『让 AI 定』」(单列 CAS UPDATE):只在门 pending
// 时置 ai_requested=true,已决的门不改。返回是否真的置上(false = 门不存在/已决)。
func (s *Service) MarkGateAIRequested(ctx context.Context, issueID string) (bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	flipped, err := MarkGateAIRequestedInTx(ctx, tx, issueID)
	if err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return flipped, nil
}

// MarkGateAIRequestedInTx 是同事务版:批量确认端点(A3)在 ai 空集合请求里用它
// 记下"请 AI 定仓"这一意图 —— 与幂等回执同一事务提交,重放拿到同状态。
func MarkGateAIRequestedInTx(ctx context.Context, tx pgx.Tx, issueID string) (bool, error) {
	command, err := tx.Exec(ctx, `UPDATE repomesh_issues.issue_discoveries
		SET scope_gate = jsonb_set(scope_gate, '{ai_requested}', 'true'::jsonb, true), updated_at = now()
		WHERE issue_id=$1 AND scope_gate ->> 'state' = 'pending'`, issueID)
	if err != nil {
		return false, err
	}
	return command.RowsAffected() == 1, nil
}

// writeGateAuditValue 只动 scope_gate 的 audit 子树(单列 jsonb_set,不走 save())：
// 查漏结论落 audit.missing(Task B3 的 PlanningGapAudit),③ 分档的越范围提示落
// audit.gap。整行写会与发现链互相丢更新,所以这里绝不走 discovery.save()。
// scope_gate 为空(老 issue 或尚未开门)时按 {} 起步,只长出 audit 子树。
func writeGateAuditValue(ctx context.Context, tx pgx.Tx, issueID, key string, value any) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("discovery: encode gate audit %s: %w", key, err)
	}
	if _, err := tx.Exec(ctx, `UPDATE repomesh_issues.issue_discoveries
		SET scope_gate = jsonb_set(COALESCE(scope_gate, '{}'::jsonb), '{audit}',
		       jsonb_set(COALESCE(scope_gate->'audit', '{}'::jsonb), ARRAY[$2::text], $3::jsonb, true), true),
		    updated_at = now()
		WHERE issue_id = $1`, issueID, key, string(payload)); err != nil {
		return fmt.Errorf("discovery: write gate audit %s: %w", key, err)
	}
	return nil
}

// PassGateAudit 是「就这样,不补」的处置(spec §3.2):单列 CAS 记
// audit.audit_passed=true,③ 于是放行。只在确实有**未处置的非空 missing**
// 且尚未通过时翻转,重复调用返回 false(不重写、不报错)。
func (s *Service) PassGateAudit(ctx context.Context, issueID string) (bool, error) {
	command, err := s.pool.Exec(ctx, `UPDATE repomesh_issues.issue_discoveries
		SET scope_gate = jsonb_set(scope_gate, '{audit}',
		       jsonb_set(scope_gate->'audit', ARRAY['audit_passed'], 'true'::jsonb, true), true),
		    updated_at = now()
		WHERE issue_id = $1
		  AND jsonb_typeof(scope_gate->'audit'->'missing') = 'array'
		  AND jsonb_array_length(scope_gate->'audit'->'missing') > 0
		  AND NOT COALESCE((scope_gate->'audit'->>'audit_passed')::bool, false)`, issueID)
	if err != nil {
		return false, err
	}
	return command.RowsAffected() == 1, nil
}

// gateAuditBlocks 读门的 audit 桶:有未处置的 missing(非空数组且未
// audit_passed)时为真 —— ③ 分档停在这里等人(补上并继续 / 就这样)。
func gateAuditBlocks(ctx context.Context, tx pgx.Tx, issueID string) (bool, error) {
	var blocked bool
	err := tx.QueryRow(ctx, `SELECT
		CASE WHEN jsonb_typeof(scope_gate->'audit'->'missing') = 'array'
		     THEN jsonb_array_length(scope_gate->'audit'->'missing') > 0
		     ELSE false END
		AND NOT COALESCE((scope_gate->'audit'->>'audit_passed')::bool, false)
		FROM repomesh_issues.issue_discoveries WHERE issue_id=$1`, issueID).Scan(&blocked)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return blocked, nil
}

// gateDeadlineForIssue 读 issues.hitl_mode 决定开门截止(spec §1):ai = 自动托管
// → now+10 分钟(无人点则自动生成+采纳);hitl = 人审 → nil,门无限等待。
func (s *Service) gateDeadlineForIssue(ctx context.Context, tx pgx.Tx, issueID string) (*time.Time, error) {
	var hitlMode string
	if err := tx.QueryRow(ctx,
		`SELECT COALESCE(hitl_mode,'hitl') FROM repomesh_issues.issues WHERE id=$1`, issueID).
		Scan(&hitlMode); err != nil {
		return nil, fmt.Errorf("discovery: 开选仓门前读 hitl 模式: %w", err)
	}
	if hitlMode == "ai" {
		at := time.Now().UTC().Add(gateAutoDeadline)
		return &at, nil
	}
	return nil, nil
}

// fillGateSuggestedForCandidates 在②候选产物落库的事务里**补建议**(门已在①分析
// 之后开出,这里只填 suggested,不再开门)。建议取候选块的 repository_name/score/
// agent_tier/rationale,构造 {repository, score, tier, reason} 对象——**排除档不写**
// (2026-09-21 用户裁定:门只发建议纳入的仓),按分数降序。已决的门由 CAS 如实跳过。
func (s *Service) fillGateSuggestedForCandidates(ctx context.Context, tx pgx.Tx, st *State, items []any) error {
	suggestions := make(suggestedJSON, 0, len(items))
	for _, itemAny := range items {
		item, _ := itemAny.(map[string]any)
		name, _ := item["repository_name"].(string)
		if strings.TrimSpace(name) == "" {
			continue
		}
		score, _ := item["score"].(float64)
		tier, _ := item["agent_tier"].(string)
		tier = strings.ToLower(strings.TrimSpace(tier))
		// agent 没给档位时按分档阈值回退出一个标签（与③同一套阈值），
		// 免得门上出现一档空白的建议行。
		if tier != "required" && tier != "maybe" && tier != "excluded" {
			switch {
			case score >= requiredBar:
				tier = "required"
			case score >= maybeBar:
				tier = "maybe"
			default:
				tier = "excluded"
			}
		}
		if tier == "excluded" {
			continue
		}
		reason, _ := item["rationale"].(string)
		suggestions = append(suggestions, GateSuggestion{Repository: name, Score: score, Tier: tier, Reason: reason})
	}
	sort.SliceStable(suggestions, func(i, j int) bool { return suggestions[i].Score > suggestions[j].Score })
	return fillGateSuggestedInTx(ctx, tx, st.IssueID, suggestions)
}

// RepositoryIDsForNamesInTx 把 owner/name 建议名映射到**本项目内**的仓库 id:
// 忽略大小写、忽略空名与项目外的名字(不编仓)。超时代选与批量确认端点的 AI
// 采纳共用同一条映射,两条路不会给出不同的范围。
func RepositoryIDsForNamesInTx(ctx context.Context, tx pgx.Tx, projectID string, names []string) ([]string, error) {
	lowered := make([]string, 0, len(names))
	for _, name := range names {
		if trimmed := strings.ToLower(strings.TrimSpace(name)); trimmed != "" {
			lowered = append(lowered, trimmed)
		}
	}
	repositories := []string{}
	if len(lowered) == 0 {
		return repositories, nil
	}
	rows, err := tx.Query(ctx, `SELECT r.id FROM repomesh_projects.repositories r
		JOIN repomesh_projects.project_repositories pr ON pr.project_id=$1 AND pr.repository_id=r.id
		WHERE lower(r.owner || '/' || r.name) = ANY($2)`, projectID, lowered)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	seen := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		if !seen[id] {
			seen[id] = true
			repositories = append(repositories, id)
		}
	}
	return repositories, rows.Err()
}

// GateTimeoutReceipt 是门超时代选的回执;与 A3 批量确认端点的回执同形
// (status/repositoryCount),同键重放 status=replayed。
type GateTimeoutReceipt struct {
	Status          string `json:"status"`
	RepositoryCount int    `json:"repositoryCount"`
}

// ResolveGateTimeout 是协调器侧的门超时代选(Task B2,spec §3.2「10 分钟无人点 →
// 自动让 AI 定」):A3 批量确认服务层的**等价物**,decided_by 记为 timeout。
func (s *Service) ResolveGateTimeout(ctx context.Context, issueID, idempotencyKey string) (GateTimeoutReceipt, error) {
	receipt, _, err := resolveGateBySuggestion(ctx, s.pool, issueID, "timeout", idempotencyKey)
	return receipt, err
}

// AdoptGateSuggestion 是协调器侧的**采纳建议为范围**(spec 2026-09-20 修订):
// 人在门上点了「让 AI 定」(ai_requested)或 ai 模式 10 分钟无人点(超时),协调器
// 都走这一条 —— 与 A3 批量确认端点**同一个服务路径的直写版**(web 端点要会话主体
// LockProjectPrincipal,协调器没有):一个事务内 建议集合→项目内仓库 id → 双表写
// (issue_repository_scope 与 issue_content_scope,整组同一把 scope_revision,语句
// 与 A3 逐字同款)→ 门单列 CAS 置 resolved(decided_by 记 ai|timeout)→ 幂等回执。
//
// 返回真正落入范围的仓库 id(**调用方用它唤醒这些仓库的团队**,spec §3.3);
// 门已 resolved 时返回 status=noop 且仓库集合为空,不重写范围。
func (s *Service) AdoptGateSuggestion(ctx context.Context, issueID, decidedBy, idempotencyKey string) (GateTimeoutReceipt, []string, error) {
	return resolveGateBySuggestion(ctx, s.pool, issueID, decidedBy, idempotencyKey)
}

// beginner 抽象"能开事务"的连接(池或已有事务):采纳路径既要在协调器里自开事务,
// 也要能被同一个事务复用(测试与后续组合)。
type beginner interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

func resolveGateBySuggestion(ctx context.Context, db beginner, issueID, decidedBy, idempotencyKey string) (GateTimeoutReceipt, []string, error) {
	tx, err := db.Begin(ctx)
	if err != nil {
		return GateTimeoutReceipt{}, nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if key := strings.TrimSpace(idempotencyKey); key != "" {
		if receipt, found, lookupErr := ScopeSelectionReceiptInTx(ctx, tx, issueID, key); lookupErr != nil {
			return GateTimeoutReceipt{}, nil, lookupErr
		} else if found {
			count, _ := receipt["repository_count"].(float64)
			return GateTimeoutReceipt{Status: "replayed", RepositoryCount: int(count)}, nil, nil
		}
	}
	var projectID string
	var raw []byte
	if err := tx.QueryRow(ctx,
		`SELECT project_id, scope_gate FROM repomesh_issues.issue_discoveries WHERE issue_id=$1`, issueID).
		Scan(&projectID, &raw); err != nil {
		return GateTimeoutReceipt{}, nil, fmt.Errorf("discovery: 选仓门采纳建议读门: %w", err)
	}
	if len(raw) == 0 {
		return GateTimeoutReceipt{}, nil, fmt.Errorf("%w: 没有选仓门可采纳建议(issue %s)", ErrConflict, issueID)
	}
	var payload gateJSON
	if err := json.Unmarshal(raw, &payload); err != nil {
		return GateTimeoutReceipt{}, nil, err
	}
	if GateState(payload.State) != GatePending {
		return GateTimeoutReceipt{Status: "noop"}, nil, nil
	}
	// 建议集合(owner/name)→ 本项目内的仓库 id;忽略大小写,不在项目内的建议跳过。
	repositories, err := RepositoryIDsForNamesInTx(ctx, tx, projectID, payload.Suggested.names())
	if err != nil {
		return GateTimeoutReceipt{}, nil, err
	}
	var operation string
	if err := tx.QueryRow(ctx,
		`SELECT creation_operation_id FROM repomesh_issues.issues WHERE id=$1`, issueID).Scan(&operation); err != nil {
		return GateTimeoutReceipt{}, nil, fmt.Errorf("discovery: 选仓门采纳建议读建项操作: %w", err)
	}
	scopeRevision := newEvidenceVersion(issueID, "scope-gate-adopt", strconv.FormatInt(time.Now().UnixNano(), 10))
	for _, repositoryID := range repositories {
		if _, err := tx.Exec(ctx, `INSERT INTO repomesh_issues.issue_repository_scope
			(issue_id, repository_id, project_id, scope_revision) VALUES ($1,$2,$3,$4)
			ON CONFLICT (issue_id, repository_id) DO UPDATE SET scope_revision = EXCLUDED.scope_revision`,
			issueID, repositoryID, projectID, scopeRevision); err != nil {
			return GateTimeoutReceipt{}, nil, err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO repomesh_issues.issue_content_scope
			(issue_id, repository_id, project_id, introduced_by_operation) VALUES ($1,$2,$3,$4)
			ON CONFLICT (issue_id, repository_id) DO NOTHING`,
			issueID, repositoryID, projectID, operation); err != nil {
			return GateTimeoutReceipt{}, nil, err
		}
	}
	if _, err := ResolveGateInTx(ctx, tx, issueID, decidedBy); err != nil {
		return GateTimeoutReceipt{}, nil, err
	}
	receipt := GateTimeoutReceipt{Status: "committed", RepositoryCount: len(repositories)}
	if err := RecordScopeSelectionInTx(ctx, tx, issueID, idempotencyKey, map[string]any{
		"status": receipt.Status, "repository_count": receipt.RepositoryCount, "decided_by": decidedBy,
	}); err != nil {
		return GateTimeoutReceipt{}, nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return GateTimeoutReceipt{}, nil, err
	}
	return receipt, repositories, nil
}

// scopeLedgerKey 是范围确认在幂等账里的键:加前缀,免与发现链各步的
// 幂等键(candidates/classify 等)撞车。
func scopeLedgerKey(key string) string { return "scope_selection:" + key }

// ScopeSelectionReceiptInTx 查范围确认的幂等账。找到 → 返回原回执
// (found=true),调用方按重放处理(200,不重写范围、不改门)。
func ScopeSelectionReceiptInTx(ctx context.Context, tx pgx.Tx, issueID, key string) (map[string]any, bool, error) {
	if key == "" {
		return nil, false, nil
	}
	var raw []byte
	err := tx.QueryRow(ctx, `SELECT idempotency_ledger -> $2::text
		FROM repomesh_issues.issue_discoveries WHERE issue_id=$1`, issueID, scopeLedgerKey(key)).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if len(raw) == 0 {
		return nil, false, nil
	}
	var receipt map[string]any
	if json.Unmarshal(raw, &receipt) != nil {
		return nil, false, nil
	}
	return receipt, true, nil
}

// RecordScopeSelectionInTx 落范围确认的幂等回执:单列 jsonb 合并,绝不走
// save()(整行写会与发现链互相丢更新)。发现链行缺失时按 issue 现场补一行
// 最小状态(与 OpenGate 同思路)。
func RecordScopeSelectionInTx(ctx context.Context, tx pgx.Tx, issueID, key string, receipt map[string]any) error {
	payload, err := json.Marshal(map[string]map[string]any{scopeLedgerKey(key): receipt})
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO repomesh_issues.issue_discoveries
		(issue_id, project_id, requirement_text, idempotency_ledger)
		SELECT i.id, i.project_id, btrim(i.title || E'\n' || i.description), $2::jsonb
		FROM repomesh_issues.issues i
		WHERE i.id = $1
		ON CONFLICT (issue_id) DO UPDATE SET
			idempotency_ledger = issue_discoveries.idempotency_ledger || EXCLUDED.idempotency_ledger,
			updated_at = now()`, issueID, string(payload))
	return err
}
