package discovery

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"repomesh.local/repomesh/internal/testdb"
)

// ③ 只对已确认范围分档(Task B3,spec §3.2「分档硬约束」):
// agent 圈了 5 个仓、人选仓门只确认了 3 个 → 分档(required/maybe)只含确认的
// 3 仓;越范围的 2 仓不进 required/maybe(否则 validateRepositories 409),
// 汇入门的 audit.gap 提示桶(单列写,不走 save)。
func TestClassificationScopedToConfirmedRange(t *testing.T) {
	pool := testdb.Open(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	const issueID = "iss_gap_scope_classify"
	all := []string{"acme/checkout", "acme/shared-lib", "acme/billing", "acme/extra-a", "acme/extra-b"}
	fixture := testdb.SeedProject(t, pool, "", "", all...)
	// 人确认的范围:前 3 仓(建项聚合种子即确认范围)。
	testdb.SeedIssue(t, pool, fixture, issueID, all[0], all[1], all[2])
	// 门已人工确认(建议是 agent 的 5 仓,人选了 3)。
	service := New(pool)
	if err := service.OpenGate(ctx, issueID, all, nil); err != nil {
		t.Fatalf("open gate: %v", err)
	}
	if _, err := service.ResolveGate(ctx, issueID, "manual"); err != nil {
		t.Fatalf("resolve gate: %v", err)
	}
	// 候选块:agent 给 5 仓全打了 required —— 老代码这里 ③ 必 409。
	entries := []any{}
	for _, name := range all {
		entries = append(entries, map[string]any{
			"repository_name": name, "score": 0.9, "agent_tier": "required", "rationale": "agent 判断必改",
		})
	}
	candidates, err := json.Marshal(map[string]any{"items": entries, "llm_used": true})
	if err != nil {
		t.Fatal(err)
	}
	analysis, err := json.Marshal(map[string]any{"sufficient": true, "analyzed_requirement": "改结算"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO repomesh_issues.issue_discoveries
		(issue_id, project_id, requirement_text, analysis, candidates, idempotency_ledger)
		VALUES ($1, $2, '改结算', $3::jsonb, $4::jsonb, '{}'::jsonb)
		ON CONFLICT (issue_id) DO UPDATE SET analysis=EXCLUDED.analysis, candidates=EXCLUDED.candidates`,
		issueID, fixture.ID, string(analysis), string(candidates)); err != nil {
		t.Fatalf("fixture: %v", err)
	}

	if _, err := service.Classification(ctx, issueID, agentID, "idem-classify-scoped"); err != nil {
		t.Fatalf("③ 分档不得再因越范围候选 409: %v", err)
	}
	classification := readJSONColumn(t, ctx, pool, "classification", issueID)
	required := namesOf(t, classification["required"])
	if len(required) != 3 {
		t.Fatalf("分档只应含已确认的 3 仓: %+v", required)
	}
	for _, name := range all[:3] {
		if !contains(required, name) {
			t.Fatalf("确认仓 %s 应在必改档: %+v", name, required)
		}
	}
	for _, name := range all[3:] {
		if contains(required, name) || contains(namesOf(t, classification["maybe"]), name) {
			t.Fatalf("越范围仓 %s 不得进 required/maybe", name)
		}
	}
	// 越范围仓汇入门的 audit.gap 桶(单列写,仍在 scope_gate 上)。
	var auditRaw []byte
	if err := pool.QueryRow(ctx, `SELECT scope_gate->'audit' FROM repomesh_issues.issue_discoveries
		WHERE issue_id=$1`, issueID).Scan(&auditRaw); err != nil {
		t.Fatalf("读 audit 桶: %v", err)
	}
	var audit struct {
		Gap []struct {
			Repository string `json:"repository"`
			Reason     string `json:"reason"`
		} `json:"gap"`
	}
	if err := json.Unmarshal(auditRaw, &audit); err != nil {
		t.Fatalf("解析 audit 桶: %v (%s)", err, string(auditRaw))
	}
	gapNames := []string{}
	for _, entry := range audit.Gap {
		gapNames = append(gapNames, entry.Repository)
	}
	if len(gapNames) != 2 || !contains(gapNames, "acme/extra-a") || !contains(gapNames, "acme/extra-b") {
		t.Fatalf("gap 桶应恰含 2 个越范围仓: %+v", gapNames)
	}
}

// 查漏步 PlanningGapAudit(Task B3,spec §3.2「查漏」):manual 确认后由协调器派,
// 产物 {missing:[{repository,reason}]} 落 gate.audit。空 missing → ③ 照常;
// 有 missing → ③ 停等人(补上或「就这样」audit_passed)后放行。
func TestPlanningGapAuditArtifactGatesClassification(t *testing.T) {
	pool := testdb.Open(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	service := New(pool)
	prov := PlanningProvenance{Role: "organization_leader", SkillID: "cross-repo-planning", RunID: "run_gap_audit"}

	seed := func(issueID string, confirmed ...string) string {
		fixture := testdb.SeedProject(t, pool, "", "", "acme/checkout", "acme/shared-lib", "acme/billing")
		testdb.SeedIssue(t, pool, fixture, issueID, confirmed...)
		if err := service.OpenGate(ctx, issueID, []string{"acme/checkout", "acme/shared-lib", "acme/billing"}, nil); err != nil {
			t.Fatalf("open gate: %v", err)
		}
		if _, err := service.ResolveGate(ctx, issueID, "manual"); err != nil {
			t.Fatalf("resolve gate: %v", err)
		}
		analysis, _ := json.Marshal(map[string]any{"sufficient": true, "analyzed_requirement": "改结算"})
		candidates, _ := json.Marshal(map[string]any{"items": []any{
			map[string]any{"repository_name": "acme/checkout", "score": 0.9, "agent_tier": "required", "rationale": "点名"},
		}, "llm_used": true})
		if _, err := pool.Exec(ctx, `INSERT INTO repomesh_issues.issue_discoveries
			(issue_id, project_id, requirement_text, analysis, candidates, idempotency_ledger)
			VALUES ($1, $2, '改结算', $3::jsonb, $4::jsonb, '{}'::jsonb)
			ON CONFLICT (issue_id) DO UPDATE SET analysis=EXCLUDED.analysis, candidates=EXCLUDED.candidates`,
			issueID, fixture.ID, string(analysis), string(candidates)); err != nil {
			t.Fatalf("fixture: %v", err)
		}
		return fixture.ID
	}

	// 有 missing:落 gate.audit.missing,③ 被拦,直到人处置。
	const blockedID = "iss_gap_audit_missing"
	seed(blockedID, "acme/checkout")
	if err := service.ApplyPlanningRun(ctx, blockedID, PlanningGapAudit, map[string]any{
		"missing": []any{map[string]any{"repository": "acme/billing", "reason": "checkout 依赖它的结算接口"}},
	}, prov); err != nil {
		t.Fatalf("ApplyPlanningRun(查漏): %v", err)
	}
	var missingRaw []byte
	if err := pool.QueryRow(ctx, `SELECT scope_gate->'audit'->'missing' FROM repomesh_issues.issue_discoveries
		WHERE issue_id=$1`, blockedID).Scan(&missingRaw); err != nil {
		t.Fatalf("读 audit.missing: %v", err)
	}
	var missing []struct {
		Repository string `json:"repository"`
		Reason     string `json:"reason"`
	}
	if err := json.Unmarshal(missingRaw, &missing); err != nil || len(missing) != 1 || missing[0].Repository != "acme/billing" {
		t.Fatalf("查漏产物应落 gate.audit.missing: %s %v", string(missingRaw), err)
	}
	_, err := service.Classification(ctx, blockedID, agentID, "idem-classify-blocked")
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("有未处置 missing 时 ③ 应停等人: %v", err)
	}
	// 「就这样」:CAS 记 audit_passed → ③ 放行。
	passed, err := service.PassGateAudit(ctx, blockedID)
	if err != nil || !passed {
		t.Fatalf("PassGateAudit 应翻转: %v %v", passed, err)
	}
	if _, err := service.Classification(ctx, blockedID, agentID, "idem-classify-blocked"); err != nil {
		t.Fatalf("audit_passed 后 ③ 应放行: %v", err)
	}

	// 空 missing:自动过门,③ 直接走。
	const emptyID = "iss_gap_audit_empty"
	seed(emptyID, "acme/checkout", "acme/shared-lib")
	if err := service.ApplyPlanningRun(ctx, emptyID, PlanningGapAudit, map[string]any{
		"missing": []any{},
	}, prov); err != nil {
		t.Fatalf("ApplyPlanningRun(空查漏): %v", err)
	}
	if _, err := service.Classification(ctx, emptyID, agentID, "idem-classify-empty"); err != nil {
		t.Fatalf("空 missing 应自动过门进③: %v", err)
	}
}

// 查漏产物的结构校验:missing 必须是数组(允许空);缺键就是不合格产物。
func TestParsePlanningArtifactGapAudit(t *testing.T) {
	if _, err := ParsePlanningArtifact(PlanningGapAudit, []byte(`{"missing":[]}`)); err != nil {
		t.Fatalf("空 missing 是合法产物: %v", err)
	}
	if _, err := ParsePlanningArtifact(PlanningGapAudit, []byte(`{"missing":[{"repository":"a/b","reason":"r"}]}`)); err != nil {
		t.Fatalf("非空 missing 是合法产物: %v", err)
	}
	if _, err := ParsePlanningArtifact(PlanningGapAudit, []byte(`{}`)); err == nil {
		t.Fatal("缺 missing 键应判不合格")
	}
}
