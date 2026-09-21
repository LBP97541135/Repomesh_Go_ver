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
	State      GateState
	DecidedBy  string // manual|ai|timeout
	Suggested  []string
	DeadlineAt *time.Time // 只在 ai 模式置(开门+10 分钟);hitl 无截止、门无限等待
	ResolvedAt *time.Time
}

// gateJSON 与列内 JSON 形状一一对应;时间用 RFC3339 文本存取。
type gateJSON struct {
	State      string     `json:"state"`
	DecidedBy  string     `json:"decided_by"`
	Suggested  []string   `json:"suggested"`
	DeadlineAt *time.Time `json:"deadline_at"`
	ResolvedAt *time.Time `json:"resolved_at"`
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
	if suggested == nil {
		suggested = []string{}
	}
	payload, err := json.Marshal(gateJSON{State: string(GatePending), Suggested: suggested, DeadlineAt: deadline})
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
	var raw []byte
	err := s.pool.QueryRow(ctx,
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
		State:      GateState(payload.State),
		DecidedBy:  payload.DecidedBy,
		Suggested:  payload.Suggested,
		DeadlineAt: payload.DeadlineAt,
		ResolvedAt: payload.ResolvedAt,
	}
	if gate.Suggested == nil {
		gate.Suggested = []string{}
	}
	return gate, nil
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

// openGateForCandidates 在②候选产物落库的事务里开选仓门:模式读 issues.hitl_mode
// (0053),ai = 自动托管 → 截止 now+10 分钟;hitl = 人审 → 无截止,门无限等待。
// 建议集合取候选块的 repository_name(全名),过滤空名。
func (s *Service) openGateForCandidates(ctx context.Context, tx pgx.Tx, st *State, items []any) error {
	var hitlMode string
	if err := tx.QueryRow(ctx,
		`SELECT COALESCE(hitl_mode,'hitl') FROM repomesh_issues.issues WHERE id=$1`, st.IssueID).
		Scan(&hitlMode); err != nil {
		return fmt.Errorf("discovery: 开选仓门前读 hitl 模式: %w", err)
	}
	var deadline *time.Time
	if hitlMode == "ai" {
		at := time.Now().UTC().Add(gateAutoDeadline)
		deadline = &at
	}
	suggested := make([]string, 0, len(items))
	for _, itemAny := range items {
		item, _ := itemAny.(map[string]any)
		name, _ := item["repository_name"].(string)
		if strings.TrimSpace(name) == "" {
			continue
		}
		suggested = append(suggested, name)
	}
	return openGateInTx(ctx, tx, st.IssueID, suggested, deadline)
}

// GateTimeoutReceipt 是门超时代选的回执;与 A3 批量确认端点的回执同形
// (status/repositoryCount),同键重放 status=replayed。
type GateTimeoutReceipt struct {
	Status          string `json:"status"`
	RepositoryCount int    `json:"repositoryCount"`
}

// ResolveGateTimeout 是协调器侧的门超时代选(Task B2,spec §3.2「10 分钟无人点 →
// 自动让 AI 定」):A3 批量确认服务层的**等价物**——web 端点要会话主体
// (LockProjectPrincipal),协调器没有,所以这里按同一形状直写:
// 一个事务内 建议集合→项目内仓库 id → 双表写(issue_repository_scope 与
// issue_content_scope,整组同一把 scope_revision,语句与 A3 逐字同款)→
// 门单列 CAS 置 resolved(timeout)→ 幂等回执落发现链单列账。
//
// 门已 resolved(被人抢先确认/已被代选)时返回 status=noop,不重写范围;
// 建议里不在本项目内的仓如实忽略,不编仓。
func (s *Service) ResolveGateTimeout(ctx context.Context, issueID, idempotencyKey string) (GateTimeoutReceipt, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return GateTimeoutReceipt{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if key := strings.TrimSpace(idempotencyKey); key != "" {
		if receipt, found, lookupErr := ScopeSelectionReceiptInTx(ctx, tx, issueID, key); lookupErr != nil {
			return GateTimeoutReceipt{}, lookupErr
		} else if found {
			count, _ := receipt["repository_count"].(float64)
			return GateTimeoutReceipt{Status: "replayed", RepositoryCount: int(count)}, nil
		}
	}
	var projectID string
	var raw []byte
	if err := tx.QueryRow(ctx,
		`SELECT project_id, scope_gate FROM repomesh_issues.issue_discoveries WHERE issue_id=$1`, issueID).
		Scan(&projectID, &raw); err != nil {
		return GateTimeoutReceipt{}, fmt.Errorf("discovery: 门超时代选读门: %w", err)
	}
	if len(raw) == 0 {
		return GateTimeoutReceipt{}, fmt.Errorf("%w: 没有选仓门可代选(issue %s)", ErrConflict, issueID)
	}
	var payload gateJSON
	if err := json.Unmarshal(raw, &payload); err != nil {
		return GateTimeoutReceipt{}, err
	}
	if GateState(payload.State) != GatePending {
		return GateTimeoutReceipt{Status: "noop"}, nil
	}
	// 建议集合(owner/name)→ 本项目内的仓库 id;忽略大小写,不在项目内的建议跳过。
	lowered := make([]string, 0, len(payload.Suggested))
	for _, name := range payload.Suggested {
		if trimmed := strings.ToLower(strings.TrimSpace(name)); trimmed != "" {
			lowered = append(lowered, trimmed)
		}
	}
	repositories := []string{}
	if len(lowered) > 0 {
		rows, err := tx.Query(ctx, `SELECT r.id FROM repomesh_projects.repositories r
			JOIN repomesh_projects.project_repositories pr ON pr.project_id=$1 AND pr.repository_id=r.id
			WHERE lower(r.owner || '/' || r.name) = ANY($2)`, projectID, lowered)
		if err != nil {
			return GateTimeoutReceipt{}, err
		}
		seen := map[string]bool{}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return GateTimeoutReceipt{}, err
			}
			if !seen[id] {
				seen[id] = true
				repositories = append(repositories, id)
			}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return GateTimeoutReceipt{}, err
		}
	}
	var operation string
	if err := tx.QueryRow(ctx,
		`SELECT creation_operation_id FROM repomesh_issues.issues WHERE id=$1`, issueID).Scan(&operation); err != nil {
		return GateTimeoutReceipt{}, fmt.Errorf("discovery: 门超时代选读建项操作: %w", err)
	}
	scopeRevision := newEvidenceVersion(issueID, "scope-gate-timeout", strconv.FormatInt(time.Now().UnixNano(), 10))
	for _, repositoryID := range repositories {
		if _, err := tx.Exec(ctx, `INSERT INTO repomesh_issues.issue_repository_scope
			(issue_id, repository_id, project_id, scope_revision) VALUES ($1,$2,$3,$4)
			ON CONFLICT (issue_id, repository_id) DO UPDATE SET scope_revision = EXCLUDED.scope_revision`,
			issueID, repositoryID, projectID, scopeRevision); err != nil {
			return GateTimeoutReceipt{}, err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO repomesh_issues.issue_content_scope
			(issue_id, repository_id, project_id, introduced_by_operation) VALUES ($1,$2,$3,$4)
			ON CONFLICT (issue_id, repository_id) DO NOTHING`,
			issueID, repositoryID, projectID, operation); err != nil {
			return GateTimeoutReceipt{}, err
		}
	}
	if _, err := ResolveGateInTx(ctx, tx, issueID, "timeout"); err != nil {
		return GateTimeoutReceipt{}, err
	}
	receipt := GateTimeoutReceipt{Status: "committed", RepositoryCount: len(repositories)}
	if err := RecordScopeSelectionInTx(ctx, tx, issueID, idempotencyKey, map[string]any{
		"status": receipt.Status, "repository_count": receipt.RepositoryCount, "decided_by": "timeout",
	}); err != nil {
		return GateTimeoutReceipt{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return GateTimeoutReceipt{}, err
	}
	return receipt, nil
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
