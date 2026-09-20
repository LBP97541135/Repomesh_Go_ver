package discovery

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"repomesh.local/repomesh/internal/testdb"
)

// 选仓门开门时机(spec 2026-09-20 §3.2 / Task B1):② PlanningCandidates 产物
// 落库的那一刻开门——建议集合就是候选全名;ai 模式带 10 分钟截止,hitl 模式无截止
// (门无限等待);重复应用产物不得开出第二扇门(开门幂等)。
func TestApplyPlanningCandidatesOpensGatePerHitlMode(t *testing.T) {
	pool := testdb.Open(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	seed := func(issueID, hitlMode string) {
		t.Helper()
		fixture := testdb.SeedProject(t, pool, "", "", "acme/checkout", "acme/shared-lib", "acme/docs-site")
		testdb.SeedIssue(t, pool, fixture, issueID, "acme/checkout", "acme/shared-lib")
		if hitlMode != "" {
			exec := func(sql string, args ...any) {
				t.Helper()
				if _, err := pool.Exec(ctx, sql, args...); err != nil {
					t.Fatalf("fixture: %v", err)
				}
			}
			exec(`UPDATE repomesh_issues.issues SET hitl_mode=$2 WHERE id=$1`, issueID, hitlMode)
		}
	}
	candidatesArtifact := func(names ...string) map[string]any {
		entries := []any{}
		for _, name := range names {
			entries = append(entries, map[string]any{
				"repository": name, "tier": "required", "score": 0.9, "reason": "需求点名了 " + name,
			})
		}
		return map[string]any{"candidates": entries}
	}
	prov := PlanningProvenance{Role: "organization_leader", SkillID: "cross-repo-planning", RunID: "run_gate_b1"}
	service := New(pool)

	// ai 模式:截止 = 开门时刻 + 10 分钟,建议 = 候选全名。
	const aiIssue = "iss_gate_open_ai"
	seed(aiIssue, "ai")
	before := time.Now().UTC()
	if err := service.ApplyPlanningRun(ctx, aiIssue, PlanningCandidates,
		candidatesArtifact("acme/checkout", "acme/shared-lib"), prov); err != nil {
		t.Fatalf("ApplyPlanningRun: %v", err)
	}
	after := time.Now().UTC()
	gate, err := service.Gate(ctx, aiIssue)
	if err != nil {
		t.Fatalf("Gate: %v", err)
	}
	if gate == nil || gate.State != GatePending {
		t.Fatalf("ai 模式候选落库应开 pending 门: %+v", gate)
	}
	if len(gate.Suggested) != 2 || gate.Suggested[0] != "acme/checkout" || gate.Suggested[1] != "acme/shared-lib" {
		t.Fatalf("建议集合应为候选全名: %+v", gate.Suggested)
	}
	if gate.DeadlineAt == nil {
		t.Fatal("ai 模式门必须带截止(10 分钟)")
	}
	if gate.DeadlineAt.Before(before.Add(10*time.Minute)) || gate.DeadlineAt.After(after.Add(10*time.Minute)) {
		t.Fatalf("截止应落在 [开门前+10m, 开门后+10m]: %v", gate.DeadlineAt)
	}

	// hitl 模式:无截止,门无限等待。
	const hitlIssue = "iss_gate_open_hitl"
	seed(hitlIssue, "")
	if err := service.ApplyPlanningRun(ctx, hitlIssue, PlanningCandidates,
		candidatesArtifact("acme/checkout"), prov); err != nil {
		t.Fatalf("ApplyPlanningRun: %v", err)
	}
	gate, err = service.Gate(ctx, hitlIssue)
	if err != nil {
		t.Fatalf("Gate: %v", err)
	}
	if gate == nil || gate.State != GatePending || gate.DeadlineAt != nil {
		t.Fatalf("hitl 模式门应 pending 且无截止: %+v", gate)
	}

	// 幂等:同一条链再次应用候选产物(重跑/重投),不得开出第二扇门或改写原门。
	if err := service.ApplyPlanningRun(ctx, hitlIssue, PlanningCandidates,
		candidatesArtifact("acme/docs-site"), prov); err != nil {
		t.Fatalf("重复 ApplyPlanningRun: %v", err)
	}
	gate, err = service.Gate(ctx, hitlIssue)
	if err != nil {
		t.Fatalf("Gate: %v", err)
	}
	if gate == nil || gate.State != GatePending || gate.DeadlineAt != nil {
		t.Fatalf("重复应用后门状态不应变化: %+v", gate)
	}
	if len(gate.Suggested) != 1 || gate.Suggested[0] != "acme/checkout" {
		t.Fatalf("重复应用不得改写原门的建议集合: %+v", gate.Suggested)
	}
	// 候选块本身照常换代(门只是不被覆盖)。
	var raw []byte
	if err := pool.QueryRow(ctx, `SELECT candidates FROM repomesh_issues.issue_discoveries WHERE issue_id=$1`, hitlIssue).Scan(&raw); err != nil {
		t.Fatalf("读候选块: %v", err)
	}
	var stored map[string]any
	if err := json.Unmarshal(raw, &stored); err != nil {
		t.Fatalf("解析候选块: %v", err)
	}
	if items, _ := stored["items"].([]any); len(items) != 1 {
		t.Fatalf("候选块应按新产物换代: %s", string(raw))
	}
}
