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

// OpenGate 打开选仓门:issue_discoveries 没有 scope_gate(行缺或列为空)才写,
// 已有门(pending 或 resolved)一律不动——重复开门幂等且不覆盖。发现链行
// 缺失时按 issue 现场补一行最小状态(与 ensureState 同思路),门不依赖
// "① 已经跑过"。
func (s *Service) OpenGate(ctx context.Context, issueID string, suggested []string, deadline *time.Time) error {
	if suggested == nil {
		suggested = []string{}
	}
	payload, err := json.Marshal(gateJSON{State: string(GatePending), Suggested: suggested, DeadlineAt: deadline})
	if err != nil {
		return err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
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
	return tx.Commit(ctx)
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
