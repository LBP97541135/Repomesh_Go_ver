package discovery

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"repomesh.local/repomesh/internal/testdb"
)

// 收产物必须**真的落库**：2026-09-20 线上实测 planning_runs 报 succeeded、
// 决策链节点也写了，但 issue_discoveries 一个字节没动 —— 状态说成功而事实没变，
// 这正是这批改动要消灭的那类"看起来在跑"。
func TestPostgresApplyPlanningRunPersistsAnalysis(t *testing.T) {
	pool := testdb.Open(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	if _, err := pool.Exec(ctx, `INSERT INTO repomesh_issues.issue_discoveries
		(issue_id, project_id, requirement_text, idempotency_ledger)
		VALUES ('iss_apply_test', '99999999-9999-4999-8999-999999999999', '报价计算增加满 6000 免运费能力', '{}'::jsonb)`); err != nil {
		t.Fatalf("fixture: %v", err)
	}
	service := New(pool)
	artifact := map[string]any{
		"dimensions": []any{
			map[string]any{"name": "业务场景", "covered": true, "note": "报价计算场景"},
			map[string]any{"name": "技术约束", "covered": false, "note": "需求未提及"},
		},
		"questions":            []any{"是否有技术约束？"},
		"extracted_keywords":   []any{"报价计算", "免运费"},
		"analyzed_requirement": "在报价计算里增加判断：小计≥6000 时运费为 0",
	}
	prov := PlanningProvenance{Role: "organization_leader", SkillID: "project-intake", RunID: "run_plan_fixture"}
	if err := service.ApplyPlanningRun(ctx, "iss_apply_test", PlanningAnalysis, artifact, prov); err != nil {
		t.Fatalf("ApplyPlanningRun: %v", err)
	}

	var raw []byte
	if err := pool.QueryRow(ctx, `SELECT analysis FROM repomesh_issues.issue_discoveries
		WHERE issue_id='iss_apply_test'`).Scan(&raw); err != nil {
		t.Fatalf("读回: %v", err)
	}
	var stored map[string]any
	if err := json.Unmarshal(raw, &stored); err != nil {
		t.Fatalf("解析: %v", err)
	}
	if stored["producer"] == nil {
		t.Fatalf("产物没落库（producer 缺失）：%s", string(raw))
	}
	if producer, _ := stored["producer"].(map[string]any); producer["run_id"] != "run_plan_fixture" {
		t.Fatalf("产出者信息不符：%+v", stored["producer"])
	}
	if sufficient, _ := stored["sufficient"].(bool); !sufficient {
		t.Fatal("改写后的需求足够长，sufficient 应为 true")
	}
	if !strings.Contains(string(raw), "小计≥6000") {
		t.Fatalf("analyzed_requirement 没落库：%s", string(raw))
	}
}
