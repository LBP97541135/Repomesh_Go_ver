package discovery

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"repomesh.local/repomesh/internal/testdb"
)

// 2026-09-21 用户裁定（③ 分档权威 + 门建议形状）的核心回归。
//
// 线上实测：人在选仓门勾了仓，③ 却直接采用 AI 的 agent_tier，把人选的仓排除光，
// 于是报「本次没有任何仓库被纳入改动（全部为「排除」）」（plan.go 的全排除守卫）。
// 这里锁住三件事：
//  1. **人在选仓门确认过的仓，即便 agent 判 excluded 也进 maybe**（取较高者）；
//  2. 门的建议只含**非排除档**，带分数/档位/理由，按分数降序；
//  3. ③ 的调整写回路（既有 Adjustment）能真的改档位。
func TestConfirmedScopeOutranksAgentTier(t *testing.T) {
	pool := testdb.Open(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	const issue = "iss_tier_authority"
	// 项目内有 checkout/shared-lib/docs-site；本 issue 的**已确认范围**只含前两个。
	seedRecallFixture(t, ctx, pool, issue, "结算流程改造", []string{"结算"})
	service := New(pool)

	// ① 分析落库即开门那一步的等价物：门先出现，② 候选落库才补建议。
	if err := service.OpenGate(ctx, issue, nil, nil); err != nil {
		t.Fatalf("OpenGate: %v", err)
	}
	// 候选：agent 把**范围内**的 checkout 判成排除、把**范围外**的 docs-site 判成必改。
	artifact := map[string]any{
		"candidates": []any{
			map[string]any{"repository": "acme/checkout", "tier": "excluded", "score": 0.10, "reason": "需求没点名"},
			map[string]any{"repository": "acme/shared-lib", "tier": "maybe", "score": 0.55, "reason": "被结算依赖"},
			map[string]any{"repository": "acme/docs-site", "tier": "required", "score": 0.93, "reason": "文档要改"},
		},
	}
	prov := PlanningProvenance{Role: "organization_leader", SkillID: "cross-repo-planning", RunID: "run_tier_authority"}
	if err := service.ApplyPlanningRun(ctx, issue, PlanningCandidates, artifact, prov); err != nil {
		t.Fatalf("ApplyPlanningRun candidates: %v", err)
	}

	// ② 门的建议：排除档不进列表；按分数降序；带分数/档位/理由。
	gate, err := service.Gate(ctx, issue)
	if err != nil || gate == nil {
		t.Fatalf("Gate: %+v %v", gate, err)
	}
	if got := gate.Suggested; len(got) != 2 || got[0] != "acme/docs-site" || got[1] != "acme/shared-lib" {
		t.Fatalf("排除档不得进门的建议，且应按分数降序: %+v", got)
	}
	if len(gate.Suggestions) != 2 {
		t.Fatalf("建议应带完整形状: %+v", gate.Suggestions)
	}
	top := gate.Suggestions[0]
	if top.Repository != "acme/docs-site" || top.Tier != "required" || top.Score != 0.93 || top.Reason != "文档要改" {
		t.Fatalf("建议行形状不符（repository/score/tier/reason）: %+v", top)
	}

	// ③ 分档：范围内的人选仓即便被 agent 排除也进 maybe；范围外的必改候选进 gap。
	if _, err := service.Classification(ctx, issue, agentID, "idem-classify"); err != nil {
		t.Fatalf("Classification: %v", err)
	}
	classification := readJSONColumn(t, ctx, pool, "classification", issue)
	required := namesOf(t, classification["required"])
	maybe := namesOf(t, classification["maybe"])
	excluded := namesOf(t, classification["excluded"])
	if contains(required, "acme/docs-site") {
		t.Fatalf("范围外的候选不得进必改档（应进 gap）: %+v", required)
	}
	if !contains(maybe, "acme/checkout") {
		t.Fatalf("人选仓即便被 agent 判排除也应进「可能」档: maybe=%+v", maybe)
	}
	if contains(excluded, "acme/checkout") {
		t.Fatalf("人选仓不得留在排除档: excluded=%+v", excluded)
	}
	var gap []map[string]any
	if err := json.Unmarshal(gateAuditKey(t, ctx, pool, issue, "gap"), &gap); err != nil {
		t.Fatalf("解析 gap 桶: %v", err)
	}
	if len(gap) != 1 || gap[0]["repository"] != "acme/docs-site" {
		t.Fatalf("越范围的必改候选应进 gap 提示桶: %+v", gap)
	}

	// ③ 的调整写回路（既有 Adjustment）：把 checkout 改成「必需」，生效分档必须跟着变。
	version := evidenceVersion(t, ctx, pool, issue)
	if _, err := service.Approval(ctx, issue, agentID, "idem-approve", "approved", "人确认范围优先",
		[]Adjustment{{Repository: "acme/checkout", Tier: "required"}}, version); err != nil {
		t.Fatalf("Approval（带调整）: %v", err)
	}
	adjusted := false
	tiers := effectiveTiersOf(t, ctx, pool, issue)
	for _, entry := range tiers {
		if entry["repository"] == "acme/checkout" {
			adjusted = entry["tier"] == "required" && entry["adjusted"] == true
		}
	}
	if !adjusted {
		t.Fatalf("Adjustment 未改掉 checkout 的档位: %+v", tiers)
	}
}

// gateAuditKey 读门的 audit 子树里的一个键（gap / missing）；列值为 NULL 时返回 nil。
func gateAuditKey(t *testing.T, ctx context.Context, pool *pgxpool.Pool, issue, key string) []byte {
	t.Helper()
	var raw []byte
	if err := pool.QueryRow(ctx,
		`SELECT scope_gate->'audit'->$2::text FROM repomesh_issues.issue_discoveries WHERE issue_id=$1`,
		issue, key).Scan(&raw); err != nil {
		t.Fatalf("读门的 audit.%s 失败: %v", key, err)
	}
	return raw
}

// effectiveTiersOf 读生效分档（effective_tiers）的原始条目。
func effectiveTiersOf(t *testing.T, ctx context.Context, pool *pgxpool.Pool, issue string) []map[string]any {
	t.Helper()
	var raw []byte
	if err := pool.QueryRow(ctx,
		`SELECT effective_tiers FROM repomesh_issues.issue_discoveries WHERE issue_id=$1`, issue).Scan(&raw); err != nil {
		t.Fatalf("读 effective_tiers 失败: %v", err)
	}
	var tiers []map[string]any
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &tiers); err != nil {
			t.Fatalf("解析 effective_tiers 失败: %v", err)
		}
	}
	return tiers
}
