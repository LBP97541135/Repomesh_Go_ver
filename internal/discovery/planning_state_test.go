package discovery

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"repomesh.local/repomesh/internal/testdb"
)

// 2026-09-20 线上实测：issue 一建好，自动托管立刻把 ① 派给 agent，产物回来写库时
// 才发现 issue_discoveries 里还没有这一行（那一行此前只有 UI 走分析步才建），整步
// 于是以「还没有发现链状态」失败 —— agent 明明 exit 0、产物也合格，界面却什么都不
// 显示。状态是**派发的前置条件**，不是派发的结果：这个用例钉住"没有状态行也能收产物"。
func TestPostgresApplyPlanningRunCreatesMissingState(t *testing.T) {
	pool := testdb.Open(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	issueID := seedIssueWithoutDiscoveryState(t, ctx, pool)
	service := New(pool)
	artifact := map[string]any{
		"dimensions": []any{
			map[string]any{"name": "业务场景", "covered": true, "note": "报价计算场景"},
			map[string]any{"name": "行为描述", "covered": true, "note": "小计≥700 时运费为 0"},
			map[string]any{"name": "变更类型", "covered": true, "note": "新增一条判断"},
			map[string]any{"name": "技术约束", "covered": false, "note": "需求未提及"},
		},
		"questions":            []any{},
		"extracted_keywords":   []any{"报价计算", "免运费"},
		"analyzed_requirement": "在报价计算里增加判断：小计≥700 时运费为 0",
	}
	prov := PlanningProvenance{Role: "organization_leader", SkillID: "project-intake", RunID: "run_plan_first"}
	if err := service.ApplyPlanningRun(ctx, issueID, PlanningAnalysis, artifact, prov); err != nil {
		t.Fatalf("没有状态行时收产物必须自己补一份状态，却失败了：%v", err)
	}

	var requirement string
	var analysis []byte
	if err := pool.QueryRow(ctx,
		`SELECT requirement_text, analysis FROM repomesh_issues.issue_discoveries WHERE issue_id=$1`,
		issueID).Scan(&requirement, &analysis); err != nil {
		t.Fatalf("补出来的状态行读不回：%v", err)
	}
	if !strings.Contains(requirement, "满 700 免运费") {
		t.Fatalf("补出来的需求文本必须照实取自 issue 行，得到：%q", requirement)
	}
	if len(analysis) == 0 || !strings.Contains(string(analysis), "小计≥700") {
		t.Fatalf("产物没落库：%s", string(analysis))
	}
}

// seedIssueWithoutDiscoveryState 造一个**只有 issue、没有发现链状态**的最小聚合，
// 复刻"issue 刚建好、规划 agent 已经被派出去"的那一刻。
func seedIssueWithoutDiscoveryState(t *testing.T, ctx context.Context, pool *pgxpool.Pool) string {
	t.Helper()
	sum := sha256.Sum256([]byte(fmt.Sprintf("planning-state-%d", time.Now().UnixNano())))
	hexSum := hex.EncodeToString(sum[:])
	uuid := func(tag string) string {
		tagged := sha256.Sum256([]byte(tag + hexSum))
		h := hex.EncodeToString(tagged[:])
		return fmt.Sprintf("%s-%s-4%s-8%s-%s", h[0:8], h[8:12], h[13:16], h[17:20], h[20:32])
	}
	ownerID := "usr_state_" + hexSum[0:12]
	organizationID := uuid("org")
	projectID := uuid("project")
	configRevision := uuid("config")
	operationID := "opr_state_" + hexSum[0:12]
	conversationID := "conv_state_" + hexSum[0:12]
	changeSetID := "cs_state_" + hexSum[0:12]
	issueID := "iss_state_" + hexSum[0:12]

	if _, err := pool.Exec(ctx, `INSERT INTO repomesh_access.accounts (id, github_id, display_name)
		VALUES ($1, floor(random()*900000000)::bigint + 100000000, 'state-owner')
		ON CONFLICT (id) DO NOTHING`, ownerID); err != nil {
		t.Fatalf("fixture account: %v", err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("fixture tx: %v", err)
	}
	seed := func(statement string, args ...any) {
		if _, err := tx.Exec(ctx, statement, args...); err != nil {
			_ = tx.Rollback(ctx)
			t.Fatalf("fixture seed: %v", err)
		}
	}
	seed(`INSERT INTO public.organizations (id, name) VALUES ($1, 'state-org')
		ON CONFLICT (id) DO NOTHING`, organizationID)
	seed(`INSERT INTO repomesh_projects.projects
		(id, owner, organization_id, name, purpose, revision, creation_context_revision, current_configuration_revision)
		VALUES ($1, $2, $3, 'state-project', 'state purpose', $4, $5, $6)`,
		projectID, ownerID, organizationID, uuid("rev"), uuid("ctx"), configRevision)
	seed(`INSERT INTO repomesh_projects.configuration_revisions
		(project_id, revision, fixed, created_by, created_at)
		VALUES ($1, $2, '{"state":true}'::jsonb, $3, clock_timestamp())`,
		projectID, configRevision, ownerID)
	seed(`INSERT INTO repomesh_issues.project_issue_counters (project_id, next_number)
		VALUES ($1, 2)`, projectID)
	seed(`INSERT INTO repomesh_issues.creation_operations
		(project_id, actor, entry, creation_id, id, schema_version)
		VALUES ($1,$2,'issue_page',$3,$4,1)`,
		projectID, ownerID, "state-key-"+hexSum[0:12], operationID)
	seed(`INSERT INTO repomesh_issues.conversations
		(id, project_id, title, created_by_operation_id, title_origin_operation_id, content_scope_revision)
		VALUES ($1,$2,'state conversation',$3,$4,'statescope')`,
		conversationID, projectID, operationID, operationID)
	seed(`INSERT INTO repomesh_issues.issues
		(id, project_id, number, title, description, criteria, revision, main_conversation_id, main_changeset_id,
		 initial_configuration_revision, creation_operation_id)
		VALUES ($1,$2, (SELECT next_number-1 FROM repomesh_issues.project_issue_counters WHERE project_id=$2),
			'报价计算增加「满 700 免运费」能力', '小计达到 700 时运费为 0', '[]'::jsonb, 'staterev', $3, $4, $5, $6)`,
		issueID, projectID, conversationID, changeSetID, configRevision, operationID)
	seed(`INSERT INTO repomesh_issues.changesets (id, project_id, issue_id, kind)
		VALUES ($1,$2,$3,'main')`, changeSetID, projectID, issueID)
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("fixture commit: %v", err)
	}
	return issueID
}
