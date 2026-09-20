package deliverymanifest

import (
	"context"
	"crypto/rand"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"repomesh.local/repomesh/internal/testdb"
)

func fixtureUUID(t *testing.T) string {
	t.Helper()
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		t.Fatalf("uuid: %v", err)
	}
	buffer[6] = (buffer[6] & 0x0f) | 0x40
	buffer[8] = (buffer[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", buffer[0:4], buffer[4:6], buffer[6:8], buffer[8:10], buffer[10:16])
}

// seedDeliveredFixture 造一份"已经交付过一轮"的最小聚合：
// 计划带两个仓库；A 有 PR + 通过的测试 + 通过的分支验证；B 没 PR、测试不过、
// 分支验证在一条迁移语句上失败 —— 清单要能分别给出结论与失败定位。
func seedDeliveredFixture(t *testing.T, pool *pgxpool.Pool) (projectID, issueID, planID, orgID string) {
	t.Helper()
	ctx := context.Background()
	stamp := time.Now().Format("150405.000000000")
	owner := "acct-dm-" + stamp
	if _, err := pool.Exec(ctx, `INSERT INTO repomesh_access.accounts (id, github_id, display_name)
		VALUES ($1, floor(random()*800000000)::bigint + 100000000, '清单夹具账号')`, owner); err != nil {
		t.Fatalf("播种账号: %v", err)
	}
	projectID = fixtureUUID(t)
	orgID = fixtureUUID(t)
	issueID = "iss_dm_" + stamp
	planID = fixtureUUID(t)
	configRevision := fixtureUUID(t)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	seed := func(statement string, args ...any) {
		if _, err := tx.Exec(ctx, statement, args...); err != nil {
			t.Fatalf("播种失败（%s）: %v", statement, err)
		}
	}
	seed(`INSERT INTO public.organizations (id, name) VALUES ($1,'清单夹具空间')`, orgID)
	seed(`INSERT INTO repomesh_projects.projects
		 (id, owner, organization_id, name, purpose, revision, creation_context_revision, current_configuration_revision)
		 VALUES ($1,$2,$3,'清单夹具项目','交付清单集成测试用项目',$4,$5,$6)`,
		projectID, owner, orgID, fixtureUUID(t), fixtureUUID(t), configRevision)
	seed(`INSERT INTO repomesh_projects.configuration_revisions
		 (project_id, revision, fixed, created_by, created_at)
		 VALUES ($1,$2,'{"fixture":true}'::jsonb,$3,clock_timestamp())`, projectID, configRevision, owner)
	conversationID := "conv-dm-" + stamp
	mainChangeSetID := "cs-dm-" + stamp
	operationID := "opr-dm-" + stamp
	seed(`INSERT INTO repomesh_issues.project_issue_counters (project_id, next_number) VALUES ($1, 2)`, projectID)
	seed(`INSERT INTO repomesh_issues.creation_operations
		(project_id, actor, entry, creation_id, id, schema_version)
		VALUES ($1,$2,'issue_page',$3,$4,1)`, projectID, owner, "dm-key-"+stamp, operationID)
	seed(`INSERT INTO repomesh_issues.conversations
		(id, project_id, title, created_by_operation_id, title_origin_operation_id, content_scope_revision)
		VALUES ($1,$2,'交付清单会话',$3,$4,'dmscope')`, conversationID, projectID, operationID, operationID)
	seed(`INSERT INTO repomesh_issues.changesets (id, project_id, issue_id, kind) VALUES ($1,$2,$3,'main')`,
		mainChangeSetID, projectID, issueID)
	seed(`INSERT INTO repomesh_issues.issues
		 (id, project_id, number, title, description, criteria, revision, main_conversation_id,
		  main_changeset_id, initial_configuration_revision, creation_operation_id)
		 VALUES ($1,$2,1,'交付清单需求','描述','[]'::jsonb,'rev-1',$3,$4,$5,$6)`,
		issueID, projectID, conversationID, mainChangeSetID, configRevision, operationID)

	repositoryIDs := map[string]string{}
	for index, name := range []string{"a", "b"} {
		repositoryID := fmt.Sprintf("repo_%020d", 700000+index)
		repositoryIDs[name] = repositoryID
		seed(`INSERT INTO repomesh_projects.repositories (id, host, github_id, owner, name)
			 VALUES ($1,'github.com',$2,'owner',$3)`, repositoryID, 800000+index, name)
		seed(`INSERT INTO repomesh_projects.project_repositories
			 (project_id, repository_id, joined_revision, joined_at) VALUES ($1,$2,'rev-1',now())`,
			projectID, repositoryID)
	}
	seed(`INSERT INTO public.plans (id, project_id, plan_version, requirement_text, execution_batches, issue_id)
		 VALUES ($1,$2,'v2','交付清单需求','[["owner/a","owner/b"]]'::jsonb,$3)`, planID, projectID, issueID)

	// 0042 的触发器要求任务必须落在**该 issue 的仓库范围**里：先写范围行（只写一次，
	// 写在任务循环之前 —— 写在循环里会撞 (issue_id, repository_id) 主键）。
	for _, name := range []string{"a", "b"} {
		seed(`INSERT INTO repomesh_issues.issue_repository_scope
			(issue_id, repository_id, project_id, scope_revision) VALUES ($1,$2,$3,'dm-scope-1')`,
			issueID, repositoryIDs[name], projectID)
	}

	taskIDs := map[string]string{}
	for index, name := range []string{"a", "b"} {
		taskID := fixtureUUID(t)
		taskIDs[name] = taskID
		seed(`INSERT INTO public.tasks (id, organization_id, project_id, plan_id, task_uid, repository_id, title, status)
			 VALUES ($1,$2,$3,$4,$5,$6,$7,'done')`,
			taskID, orgID, projectID, planID, fmt.Sprintf("uid-%d", index), "owner/"+name, "任务 "+name)
	}
	// A：变更集 + PR + push 记录（带 sha/branch）
	changeSetID := fixtureUUID(t)
	seed(`INSERT INTO public.change_sets (id, organization_id, task_id, status, pr_url, branch)
		 VALUES ($1,$2,$3,'frozen','https://github.com/owner/a/pull/7','repomesh/auto-att-1')`,
		changeSetID, orgID, taskIDs["a"])
	seed(`INSERT INTO public.scm_commands (id, change_set_id, command_type, params, status)
		 VALUES (gen_random_uuid(),$1,'push','{"pr":"https://github.com/owner/a/pull/7","sha":"deadbeefcafe","branch":"repomesh/auto-att-1"}'::jsonb,'recorded')`,
		changeSetID)
	// A：测试证据（通过）；B：测试证据（不通过）
	seed(`INSERT INTO public.test_evidence
		 (id, project_id, issue_id, plan_id, task_id, repository_id, kind, script, command, exit_code, passed, summary, run_id, producer)
		 VALUES (gen_random_uuid(),$1,$2,$3,$4,'owner/a','task_single_point','t.sh','bash t.sh',0,true,'单点验收通过','run-a','test_agent')`,
		projectID, issueID, planID, taskIDs["a"])
	seed(`INSERT INTO public.test_evidence
		 (id, project_id, issue_id, plan_id, task_id, repository_id, kind, script, command, exit_code, passed, summary, run_id, producer)
		 VALUES (gen_random_uuid(),$1,$2,$3,$4,'owner/b','task_single_point','t.sh','bash t.sh',1,false,'断言不成立：免运费字段缺失','run-b','test_agent')`,
		projectID, issueID, planID, taskIDs["b"])
	// A：分支验证通过；B：分支验证在一条迁移语句上失败（失败尝试要留在清单里）
	seed(`INSERT INTO public.database_branch_validations
		 (id, organization_id, project_id, repository_id, candidate_sha, source_database_ref, provider,
		  provider_branch_ref, idempotency_key, status, results)
		 VALUES (gen_random_uuid(),$1,$2,$3,'deadbeefcafe','business_baseline','polardb-agentic-branch',
		  'branch_deadbeef_1','dbv-a','passed','[{"statement":"ALTER TABLE quotes ADD COLUMN free_shipping boolean","ok":true}]'::jsonb)`,
		orgID, projectID, repositoryIDs["a"])
	seed(`INSERT INTO public.database_branch_validations
		 (id, organization_id, project_id, repository_id, candidate_sha, source_database_ref, provider,
		  provider_branch_ref, idempotency_key, status, failure_code, results)
		 VALUES (gen_random_uuid(),$1,$2,$3,'cafebabe0000','business_baseline','polardb-agentic-branch',
		  'branch_cafebabe_1','dbv-b','failed','MIGRATION_FAILED',
		  '[{"statement":"ALTER TABLE quotes ADD COLUMN free_shipping boolean NOT NULL","ok":false,"error":"column contains null values"}]'::jsonb)`,
		orgID, projectID, repositoryIDs["b"])
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}
	return projectID, issueID, planID, orgID
}

func TestBuildManifestKeepsPerRepositoryVerdicts(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()
	projectID, issueID, planID, _ := seedDeliveredFixture(t, pool)
	service := New(pool)

	manifest, err := service.Build(ctx, BuildCommand{
		ProjectID: projectID, IssueID: issueID, PlanID: planID,
		CreatedBy: "human_owner", IdempotencyKey: "manifest-1",
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if manifest.Status != "inconsistent" {
		t.Fatalf("整份清单状态 = %q，应为 inconsistent（B 仓失败）", manifest.Status)
	}
	if manifest.PlanVersion != "v2" || len(manifest.Entries) != 2 {
		t.Fatalf("清单 = 版本 %q / %d 个仓库", manifest.PlanVersion, len(manifest.Entries))
	}
	byName := map[string]EntryView{}
	for _, entry := range manifest.Entries {
		byName[entry.RepositoryName] = entry
	}
	a, ok := byName["owner/a"]
	if !ok {
		t.Fatalf("缺 owner/a 那一行：%+v", manifest.Entries)
	}
	if a.PullRequestURL == "" || a.CommitSHA != "deadbeefcafe" || a.BranchRef == "" {
		t.Fatalf("A 的代码侧不完整：%+v", a)
	}
	if a.ValidationStatus != "passed" || a.DatabaseProvider != "polardb-agentic-branch" || len(a.Migrations) != 1 {
		t.Fatalf("A 的数据库侧不完整：%+v", a)
	}
	if len(a.TestEvidence) != 1 || !a.TestEvidence[0].Passed || a.FailureStage != "" {
		t.Fatalf("A 应当无失败定位：%+v", a)
	}

	b := byName["owner/b"]
	if b.FailureStage != "database" {
		t.Fatalf("B 的失败阶段 = %q，应为 database", b.FailureStage)
	}
	if b.FailureDetail == "" || b.DatabaseBaseline != "business_baseline" {
		t.Fatalf("B 的失败明细/基线缺失：%+v", b)
	}
	if len(b.TestEvidence) != 1 || b.TestEvidence[0].Passed {
		t.Fatalf("B 的测试证据应记为不通过：%+v", b.TestEvidence)
	}

	// 幂等：同键重放返回**同一份**快照，不重算、不覆盖。
	replay, err := service.Build(ctx, BuildCommand{
		ProjectID: projectID, IssueID: issueID, PlanID: planID,
		CreatedBy: "human_owner", IdempotencyKey: "manifest-1",
	})
	if err != nil {
		t.Fatalf("重放: %v", err)
	}
	if replay.ID != manifest.ID {
		t.Fatalf("重放拿到了新快照（%s ≠ %s）—— 幂等键没生效", replay.ID, manifest.ID)
	}

	// 换键重跑 → 落**新的一份**，旧的那份原样留着（失败尝试不丢）。
	second, err := service.Build(ctx, BuildCommand{
		ProjectID: projectID, IssueID: issueID, PlanID: planID,
		CreatedBy: "human_owner", IdempotencyKey: "manifest-2",
	})
	if err != nil {
		t.Fatalf("第二次 Build: %v", err)
	}
	if second.ID == manifest.ID {
		t.Fatal("换键应当落新的一份")
	}
	latest, err := service.Latest(ctx, projectID, issueID)
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	if latest.ID != second.ID {
		t.Fatalf("Latest 应返回最新那份：%s ≠ %s", latest.ID, second.ID)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM public.delivery_manifests WHERE issue_id=$1`, issueID).Scan(&count); err != nil {
		t.Fatalf("清点数: %v", err)
	}
	if count != 2 {
		t.Fatalf("清单条数 = %d，应为 2（旧的那份必须留着）", count)
	}
}
