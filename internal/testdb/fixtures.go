package testdb

import (
	"context"
	"fmt"
	"hash/fnv"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type ProjectFixture struct {
	ID, OrganizationID, OwnerID, ConfigurationID string
	Repositories                                 map[string]string
}

// SeedProject supplies actual ownership and membership rows for database tests.
func SeedProject(t testing.TB, pool *pgxpool.Pool, projectID, organizationID string, repositories ...string) ProjectFixture {
	t.Helper()
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	f := ProjectFixture{Repositories: map[string]string{}}
	if err := tx.QueryRow(ctx, `SELECT COALESCE(NULLIF($1,'')::uuid,gen_random_uuid())::text,
		COALESCE(NULLIF($2,'')::uuid,gen_random_uuid())::text,gen_random_uuid()::text`, projectID, organizationID).Scan(&f.ID, &f.OrganizationID, &f.ConfigurationID); err != nil {
		t.Fatal(err)
	}
	f.OwnerID = "fixture-" + f.ID
	fixtureExec(t, tx, `INSERT INTO public.organizations(id,name) VALUES($1,'Scope fixture') ON CONFLICT DO NOTHING`, f.OrganizationID)
	fixtureExec(t, tx, `INSERT INTO repomesh_access.accounts(id,github_id,display_name,organization_id)
		VALUES($1,$2,'Scope fixture',$3)`, f.OwnerID, fixtureExternalID(f.OwnerID), f.OrganizationID)
	fixtureExec(t, tx, `INSERT INTO repomesh_projects.projects(id,owner,organization_id,name,purpose,revision,creation_context_revision,current_configuration_revision)
		VALUES($1,$2,$3,'Scope fixture','Test repository boundaries',$4,$4,$4)`, f.ID, f.OwnerID, f.OrganizationID, f.ConfigurationID)
	fixtureExec(t, tx, `INSERT INTO repomesh_projects.configuration_revisions(project_id,revision,fixed,created_by,created_at)
		VALUES($1,$2,'{}',$3,clock_timestamp())`, f.ID, f.ConfigurationID, f.OwnerID)
	for _, fullName := range repositories {
		owner, name, ok := strings.Cut(fullName, "/")
		if !ok {
			owner, name = "fixture", fullName
		}
		external := fixtureExternalID(owner + "/" + name)
		id := fmt.Sprintf("repo_%020d", external)
		fixtureExec(t, tx, `INSERT INTO repomesh_projects.repositories(id,host,github_id,owner,name)
			VALUES($1,'github.com',$2,$3,$4) ON CONFLICT DO NOTHING`, id, external, owner, name)
		fixtureExec(t, tx, `INSERT INTO repomesh_projects.project_repositories(project_id,repository_id,joined_revision,joined_at)
			VALUES($1,$2,$3,now())`, f.ID, id, f.ConfigurationID)
		f.Repositories[fullName] = id
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	return f
}

// SeedIssue writes the complete creation aggregate without disabling constraints.
// External authorization is intentionally outside this database-only fixture.
func SeedIssue(t testing.TB, pool *pgxpool.Pool, f ProjectFixture, issueID string, repositories ...string) {
	t.Helper()
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	op, conv, cs := issueID+"-op", issueID+"-conv", issueID+"-cs"
	fixtureExec(t, tx, `INSERT INTO repomesh_issues.creation_operations(project_id,actor,entry,creation_id,id,schema_version,
		canonical_input,exact_input,issue_id,main_changeset_id,conversation_id,initial_configuration_revision,receipt)
		VALUES($1,$2,'issue_page',$3,$3,1,'{}','{}',$4,$5,$6,$7,
		jsonb_build_object('issueId',$4::text,'mainChangesetId',$5::text,'conversationId',$6::text,'initialConfigurationRevision',$7::text))`, f.ID, f.OwnerID, op, issueID, cs, conv, f.ConfigurationID)
	fixtureExec(t, tx, `INSERT INTO repomesh_issues.conversations(id,project_id,title,created_by_operation_id,title_origin_operation_id,content_scope_revision)
		VALUES($1,$2,'Scope fixture',$3,$3,'scope-1')`, conv, f.ID, op)
	fixtureExec(t, tx, `INSERT INTO repomesh_issues.issues(id,project_id,number,title,description,criteria,revision,main_conversation_id,main_changeset_id,initial_configuration_revision,creation_operation_id)
		VALUES($1,$2,(SELECT COALESCE(max(number),0)+1 FROM repomesh_issues.issues WHERE project_id=$2),'Scope fixture','Scope fixture','[]','revision-1',$3,$4,$5,$6)`, issueID, f.ID, conv, cs, f.ConfigurationID, op)
	fixtureExec(t, tx, `INSERT INTO repomesh_issues.changesets(id,project_id,issue_id,kind) VALUES($1,$2,$3,'main')`, cs, f.ID, issueID)
	for _, name := range repositories {
		id, ok := f.Repositories[name]
		if !ok {
			t.Fatalf("repository %q is not in fixture project", name)
		}
		fixtureExec(t, tx, `INSERT INTO repomesh_issues.issue_repository_scope(issue_id,project_id,repository_id,scope_revision) VALUES($1,$2,$3,'scope-1')`, issueID, f.ID, id)
		fixtureExec(t, tx, `INSERT INTO repomesh_issues.issue_content_scope(issue_id,project_id,repository_id,introduced_by_operation) VALUES($1,$2,$3,$4)`, issueID, f.ID, id, op)
		fixtureExec(t, tx, `INSERT INTO repomesh_issues.conversation_content_scope(conversation_id,project_id,repository_id,introduced_by_operation) VALUES($1,$2,$3,$4)`, conv, f.ID, id, op)
	}
	fixtureExec(t, tx, `INSERT INTO repomesh_issues.page_sources(operation_id,project_id,issue_id,conversation_id) VALUES($1,$2,$3,$4)`, op, f.ID, issueID, conv)
	fixtureExec(t, tx, `INSERT INTO repomesh_issues.conversation_cards(source_id,operation_id,project_id,issue_id,conversation_id) VALUES($1,$1,$2,$3,$4)`, op, f.ID, issueID, conv)
	fixtureExec(t, tx, `INSERT INTO repomesh_issues.continuation_work(work_id,project_id,issue_id,cause_operation_id,kind,state,reason)
		VALUES($1,$2,$3,$4,'issue_continue','blocked','INTEGRATION_NOT_AVAILABLE')`, issueID+"-work", f.ID, issueID, op)
	fixtureExec(t, tx, `INSERT INTO repomesh_issues.issue_event_streams(issue_id,project_id,generation,last_sequence) VALUES($1,$2,1,1)`, issueID, f.ID)
	fixtureExec(t, tx, `INSERT INTO repomesh_issues.issue_events(issue_id,generation,sequence,kind) VALUES($1,1,1,'snapshot_invalidated')`, issueID)
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
}

func fixtureExternalID(s string) int64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(s))
	return int64(h.Sum64()>>2) + 1
}
func fixtureExec(t testing.TB, tx pgx.Tx, query string, args ...any) {
	t.Helper()
	if _, err := tx.Exec(context.Background(), query, args...); err != nil {
		t.Fatal(err)
	}
}
