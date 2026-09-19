package discovery

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"repomesh.local/repomesh/internal/testdb"
)

// agentID 必须是合法 UUID：public.plans.created_by_agent_id 是 uuid 列，
// 传 "agent-1" 这类非 UUID 字符串会被 Postgres 直接拒绝（22P02）。
const agentID = "44444444-4444-4444-8444-444444444444"

// 端到端（真实 Postgres）：候选召回 → 分档 → 审批 → 生成计划。
//
// 锁住 2026-09-19 这一轮修的五个点：
//  1. 候选读的是**扫描产出的 AutoCard**，不再只有仓库名/描述可比；
//  2. 候选池只包含本 Issue 明确选择的项目仓库；
//  3. 没有模型供应商时**如实回退关键词**并标 llm_used=false；
//  4. 图推理把依赖补进"可能"档；
//  5. 全部排除时**拒绝生成空计划**（ErrNoRepositories）。
func TestPostgresDiscoveryChainRecallAndBottomLine(t *testing.T) {
	pool := testdb.Open(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	seedRecallFixture(t, ctx, pool, "iss_recall_hit",
		"我需要新增一个用户满1000减200的活动",
		[]string{"checkout", "满减", "促销", "结算"})
	service := New(pool) // 不接 secrets：走关键词回退路径（未配置模型时的真实形态）

	// ① 候选召回：需求文本里**没有仓库名**，只有名片里的目录/依赖/提交能对上。
	if _, err := service.Candidates(ctx, "iss_recall_hit", agentID, "idem-candidates", 20, nil); err != nil {
		t.Fatalf("Candidates: %v", err)
	}
	candidates := readJSONColumn(t, ctx, pool, "candidates", "iss_recall_hit")
	if used, _ := candidates["llm_used"].(bool); used {
		t.Fatal("没有模型供应商时必须标 llm_used=false，不能冒充模型打分")
	}
	items, _ := candidates["items"].([]any)
	if len(items) == 0 {
		t.Fatal("候选为空：AutoCard 里的目录/依赖/提交没有被用于匹配")
	}
	top, _ := items[0].(map[string]any)
	if name, _ := top["repository_name"].(string); name != "acme/checkout" {
		t.Fatalf("候选排序不符，首位是 %v", top["repository_name"])
	}
	if score, _ := top["score"].(float64); score < requiredBar {
		t.Fatalf("命中名片里的目录与依赖，置信度应达必改档，得到 %v", score)
	}
	if auto, _ := top["auto_card"].(bool); !auto {
		t.Fatal("该项应标记 auto_card=true（读到了扫描名片）")
	}

	// ② 分档：必改 + 依赖补进来的"可能"。
	if _, err := service.Classification(ctx, "iss_recall_hit", agentID, "idem-classify"); err != nil {
		t.Fatalf("Classification: %v", err)
	}
	classification := readJSONColumn(t, ctx, pool, "classification", "iss_recall_hit")
	required := namesOf(t, classification["required"])
	if len(required) == 0 || required[0] != "acme/checkout" {
		t.Fatalf("必改档不符: %+v", required)
	}
	maybe := namesOf(t, classification["maybe"])
	if !contains(maybe, "acme/shared-lib") {
		t.Fatalf("依赖图应把 acme/shared-lib 补进「可能」档，得到 %+v", maybe)
	}

	// ③ 审批 → ④ 生成计划：计划里必须真的有仓库。
	version := evidenceVersion(t, ctx, pool, "iss_recall_hit")
	if _, err := service.Approval(ctx, "iss_recall_hit", agentID, "idem-approve", "approved", "确认", nil, version); err != nil {
		t.Fatalf("Approval: %v", err)
	}
	if _, err := service.Plan(ctx, "iss_recall_hit", agentID, "idem-plan"); err != nil {
		t.Fatalf("Plan: %v", err)
	}
	plan := readJSONColumn(t, ctx, pool, "plan", "iss_recall_hit")
	repositories, _ := plan["repositories"].([]any)
	if len(repositories) == 0 {
		t.Fatal("计划里没有仓库：兜底没生效")
	}
}

// 全部候选都够不上门槛时**不能**生成空计划——必须明确拒绝并说清怎么自救。
func TestPostgresDiscoveryChainRefusesEmptyPlan(t *testing.T) {
	pool := testdb.Open(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	seedRecallFixture(t, ctx, pool, "iss_recall_miss",
		"把办公室的绿植换成多肉",
		[]string{"绿植", "多肉", "办公室"})
	service := New(pool)

	if _, err := service.Candidates(ctx, "iss_recall_miss", agentID, "idem-candidates", 20, nil); err != nil {
		t.Fatalf("Candidates: %v", err)
	}
	if _, err := service.Classification(ctx, "iss_recall_miss", agentID, "idem-classify"); err != nil {
		t.Fatalf("Classification: %v", err)
	}
	version := evidenceVersion(t, ctx, pool, "iss_recall_miss")
	_, err := service.Approval(ctx, "iss_recall_miss", agentID, "idem-approve", "approved", "确认", nil, version)
	if !errors.Is(err, ErrNoRepositories) {
		t.Fatalf("全部排除时审批应返回 ErrNoRepositories，得到 %v", err)
	}
	// 审批路径先于生成计划触发，两条消息措辞不同，但都必须告诉用户**怎么自救**
	// （把某个仓库调成「必需」或「可能」）。
	if !strings.Contains(err.Error(), "必需") {
		t.Fatalf("错误信息要告诉用户怎么自救，得到 %q", err.Error())
	}
}

// ---- fixture ----

// seedRecallFixture includes real project, issue, scope and scan rows.
func seedRecallFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool, issueID, requirement string, keywords []string) {
	t.Helper()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("fixture 播种失败: %v\nSQL: %s", err, sql)
		}
	}
	f := testdb.SeedProject(t, pool, "99999999-9999-4999-8999-999999999999", "", "acme/checkout", "acme/shared-lib", "acme/docs-site")
	testdb.SeedIssue(t, pool, f, issueID, "acme/checkout", "acme/shared-lib")
	testdb.SeedProject(t, pool, "", "", "outside/checkout-secret")
	// 扫描名片：checkout 的名片里写着结算目录、依赖 shared-lib、最近的满减提交。
	exec(`INSERT INTO repomesh_scan.repositories
		(id, name, url, description, topics, languages, fingerprint, profiled_at, metadata, test_commands, test_paths, poll_failures)
		VALUES ('scan-checkout','checkout','https://github.com/acme/checkout','结账与促销服务',
		'["payments"]'::jsonb,'["Go"]'::jsonb,'fp-checkout', now(),
		'{"topDirs":["src/checkout"],"deps":["acme/shared-lib"],"depEvidence":[{"name":"acme/shared-lib","mechanism":"BUILD","confidence":"confirmed"}],"identities":["acme/checkout"],"deployIdentities":[],"recentCommits":["feat: 满减活动"],"exposedApis":["POST /checkout"],"lowSignal":false}'::jsonb,
		'[]'::jsonb,'[]'::jsonb,0)`)
	exec(`INSERT INTO repomesh_scan.repositories
		(id, name, url, description, topics, languages, fingerprint, profiled_at, metadata, test_commands, test_paths, poll_failures)
		VALUES ('scan-shared','shared-lib','https://github.com/acme/shared-lib','共享库',
		'[]'::jsonb,'["Go"]'::jsonb,'fp-shared', now(),
		'{"topDirs":["pkg"],"deps":[],"depEvidence":[],"identities":["acme/shared-lib"],"deployIdentities":[],"recentCommits":[],"exposedApis":[],"lowSignal":false}'::jsonb,
		'[]'::jsonb,'[]'::jsonb,0)`)
	exec(`UPDATE repomesh_scan.repositories SET organization_id=$1::uuid`, f.OrganizationID)
	// 发现链状态：分析步已经跑过，关键词直接给足（跳过需求分析，只测召回之后的部分）。
	analysis, err := json.Marshal(map[string]any{
		"sufficient":           true,
		"extracted_keywords":   keywords,
		"analyzed_requirement": requirement,
	})
	if err != nil {
		t.Fatalf("构造分析状态失败: %v", err)
	}
	exec(`INSERT INTO repomesh_issues.issue_discoveries
		(issue_id, project_id, requirement_text, analysis, idempotency_ledger)
		VALUES ($1, '99999999-9999-4999-8999-999999999999', $2, $3::jsonb, '{}'::jsonb)`,
		issueID, requirement, string(analysis))
}

// ---- 读面小工具 ----

func readJSONColumn(t *testing.T, ctx context.Context, pool *pgxpool.Pool, column, issueID string) map[string]any {
	t.Helper()
	var raw []byte
	if err := pool.QueryRow(ctx, `SELECT `+column+` FROM repomesh_issues.issue_discoveries WHERE issue_id=$1`, issueID).Scan(&raw); err != nil {
		t.Fatalf("读取 %s 失败: %v", column, err)
	}
	out := map[string]any{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatalf("解析 %s 失败: %v", column, err)
		}
	}
	return out
}

func evidenceVersion(t *testing.T, ctx context.Context, pool *pgxpool.Pool, issueID string) string {
	t.Helper()
	var version *string
	if err := pool.QueryRow(ctx, `SELECT classification_evidence_version FROM repomesh_issues.issue_discoveries WHERE issue_id=$1`, issueID).Scan(&version); err != nil {
		t.Fatalf("读取证据版本失败: %v", err)
	}
	if version == nil {
		t.Fatal("分类步没有写证据版本")
	}
	return *version
}

func namesOf(t *testing.T, raw any) []string {
	t.Helper()
	entries, _ := raw.([]any)
	names := []string{}
	for _, entry := range entries {
		if item, ok := entry.(map[string]any); ok {
			if name, ok := item["repository"].(string); ok {
				names = append(names, name)
			}
		}
	}
	return names
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
