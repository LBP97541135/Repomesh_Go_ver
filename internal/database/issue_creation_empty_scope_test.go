package database

import (
	"testing"
)

// 空仓建项(spec 2026-09-20 §3.1):建 issue 不再选仓,范围由 ① 需求分析后的选仓门
// 后置确认。建项聚合触发器 creation_aggregate_complete(0017)此前要求已提交操作的
// issue **至少一行 issue_content_scope**,于是空仓建项在提交时撞
// check_violation('empty issue content scope for operation …')——线上表现为
// 「服务端暂时不可用(HTTP 500)」。0059 放宽这条语义后,零仓建项必须提交成功。
//
// 这条用真库跑完整建项聚合(与 testdb.SeedIssue 同形状,只是**不写任何 scope 行**)。
func TestIssueCreationAllowsEmptyRepositoryScope(t *testing.T) {
	db, err := Open(testContext(t), isolatedDatabase(t))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Migrate(testContext(t)); err != nil {
		t.Fatal(err)
	}
	ctx := testContext(t)
	const (
		orgID    = "90000000-0000-4000-8000-000000000101"
		project  = "90000000-0000-4000-8000-000000000102"
		revision = "90000000-0000-4000-8000-000000000103"
		owner    = "empty-scope-owner"
	)
	tx, err := db.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := tx.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("fixture: %v\nSQL: %s", err, sql)
		}
	}
	exec(`INSERT INTO public.organizations(id,name) VALUES($1,'empty-scope')`, orgID)
	exec(`INSERT INTO repomesh_access.accounts(id,github_id,display_name,organization_id)
		VALUES($1,987654321,'empty-scope',$2)`, owner, orgID)
	exec(`INSERT INTO repomesh_projects.projects(id,owner,organization_id,name,purpose,revision,creation_context_revision,current_configuration_revision)
		VALUES($1,$2,$3,'empty-scope','空仓建项',$4,$4,$4)`, project, owner, orgID, revision)
	exec(`INSERT INTO repomesh_projects.configuration_revisions(project_id,revision,fixed,created_by,created_at)
		VALUES($1,$2,'{}',$3,clock_timestamp())`, project, revision, owner)

	// 完整建项聚合:operation + conversation + issue + changeset + page source +
	// card + blocked continuation + 事件流;唯一缺的就是三类 scope 行。
	exec(`INSERT INTO repomesh_issues.creation_operations(project_id,actor,entry,creation_id,id,schema_version,
		canonical_input,exact_input,issue_id,main_changeset_id,conversation_id,initial_configuration_revision,receipt)
		VALUES($1,$2,'issue_page','empty-op','empty-op',1,'{}','{}','empty-issue','empty-cs','empty-conv',$3,
		jsonb_build_object('issueId','empty-issue','mainChangesetId','empty-cs','conversationId','empty-conv','initialConfigurationRevision',$3::text))`,
		project, owner, revision)
	exec(`INSERT INTO repomesh_issues.conversations(id,project_id,title,created_by_operation_id,title_origin_operation_id,content_scope_revision)
		VALUES('empty-conv',$1,'空仓建项','empty-op','empty-op','scope-1')`, project)
	exec(`INSERT INTO repomesh_issues.issues(id,project_id,number,title,description,criteria,revision,
		main_conversation_id,main_changeset_id,initial_configuration_revision,creation_operation_id)
		VALUES('empty-issue',$1,1,'空仓建项','建项不选仓','[]','revision-1','empty-conv','empty-cs',$2,'empty-op')`,
		project, revision)
	exec(`INSERT INTO repomesh_issues.changesets(id,project_id,issue_id,kind) VALUES('empty-cs',$1,'empty-issue','main')`, project)
	exec(`INSERT INTO repomesh_issues.page_sources(operation_id,project_id,issue_id,conversation_id)
		VALUES('empty-op',$1,'empty-issue','empty-conv')`, project)
	exec(`INSERT INTO repomesh_issues.conversation_cards(source_id,operation_id,project_id,issue_id,conversation_id)
		VALUES('empty-op','empty-op',$1,'empty-issue','empty-conv')`, project)
	exec(`INSERT INTO repomesh_issues.continuation_work(work_id,project_id,issue_id,cause_operation_id,kind,state,reason)
		VALUES('empty-work',$1,'empty-issue','empty-op','issue_continue','blocked','INTEGRATION_NOT_AVAILABLE')`, project)
	exec(`INSERT INTO repomesh_issues.issue_event_streams(issue_id,project_id,generation,last_sequence)
		VALUES('empty-issue',$1,1,1)`, project)
	exec(`INSERT INTO repomesh_issues.issue_events(issue_id,generation,sequence,kind)
		VALUES('empty-issue',1,1,'snapshot_invalidated')`)

	// 聚合的完整性检查是 DEFERRABLE INITIALLY DEFERRED 约束触发器:提交这一刻才跑。
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("空仓建项提交应成功(范围由选仓门后置确认): %v", err)
	}
	var scopeRows int
	if err := db.pool.QueryRow(ctx, `SELECT count(*) FROM repomesh_issues.issue_content_scope WHERE issue_id='empty-issue'`).Scan(&scopeRows); err != nil {
		t.Fatal(err)
	}
	if scopeRows != 0 {
		t.Fatalf("空仓建项不应有内容范围行,得到 %d", scopeRows)
	}
	var exists bool
	if err := db.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM repomesh_issues.issues WHERE id='empty-issue')`).Scan(&exists); err != nil || !exists {
		t.Fatalf("空仓建项后 issue 应存在: %v %v", exists, err)
	}
}
