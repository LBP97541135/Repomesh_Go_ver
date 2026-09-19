package database

import (
	"fmt"
	"testing"
)

func TestTaskScopeUpgradePreservesAndQuarantinesLegacyRows(t *testing.T) {
	for _, from := range []int{39, 41} {
		t.Run(fmt.Sprintf("from_%d", from), func(t *testing.T) {
			testTaskScopeUpgrade(t, from)
		})
	}
}

func testTaskScopeUpgrade(t *testing.T, from int) {
	ctx := testContext(t)
	db, err := Open(ctx, isolatedDatabase(t))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	all := db.migrations
	db.migrations = all[:from]
	if _, err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	// Legacy data contains one valid alias and one unrelated repository; upgrade
	// must preserve both payloads and never turn the latter into project membership.
	_, err = db.pool.Exec(ctx, `
 INSERT INTO public.organizations(id,name) VALUES('80000000-0000-4000-8000-000000000001','scope');
 INSERT INTO repomesh_access.accounts(id,github_id,display_name,organization_id)
 VALUES('scope-owner',987654321,'scope','80000000-0000-4000-8000-000000000001');
 INSERT INTO repomesh_projects.projects(id,owner,organization_id,name,purpose,revision,creation_context_revision,current_configuration_revision)
 VALUES('80000000-0000-4000-8000-000000000002','scope-owner','80000000-0000-4000-8000-000000000001','scope','scope','80000000-0000-4000-8000-000000000005','80000000-0000-4000-8000-000000000005','80000000-0000-4000-8000-000000000005');
 INSERT INTO repomesh_projects.configuration_revisions(project_id,revision,fixed,created_by,created_at)
 VALUES('80000000-0000-4000-8000-000000000002','80000000-0000-4000-8000-000000000005','{}','scope-owner',now());
 INSERT INTO repomesh_projects.repositories(id,host,github_id,owner,name)
 VALUES('repo_00000000000000000041','github.com',41,'org','allowed'),('repo_00000000000000000042','github.com',42,'org','outside');
 INSERT INTO repomesh_projects.project_repositories(project_id,repository_id,joined_revision,joined_at)
 VALUES('80000000-0000-4000-8000-000000000002','repo_00000000000000000041','80000000-0000-4000-8000-000000000005',now());
 INSERT INTO public.tasks(id,organization_id,project_id,repository_id,task_uid,title,idempotency_key)
 VALUES('80000000-0000-4000-8000-000000000003','80000000-0000-4000-8000-000000000001','80000000-0000-4000-8000-000000000002','org/allowed','legacy-good','good','legacy-good'),
 ('80000000-0000-4000-8000-000000000004','80000000-0000-4000-8000-000000000001','80000000-0000-4000-8000-000000000002','org/outside','legacy-bad','bad','legacy-bad');`)
	if err != nil {
		t.Fatal(err)
	}
	db.migrations = all
	if _, err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	var tasks, membership, bindings, bad int
	if err = db.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM public.tasks),
 (SELECT count(*) FROM repomesh_projects.project_repositories),
 (SELECT count(*) FROM public.task_repository_scopes),
 (SELECT count(*) FROM public.task_repository_scope_violations WHERE repository_id='org/outside')`).Scan(&tasks, &membership, &bindings, &bad); err != nil {
		t.Fatal(err)
	}
	if tasks != 2 || membership != 1 || bindings != 1 || bad != 1 {
		t.Fatalf("upgrade changed history or scope: %d %d %d %d", tasks, membership, bindings, bad)
	}
	if _, err = db.pool.Exec(ctx, `UPDATE public.tasks SET repository_id='org/outside' WHERE task_uid='legacy-good'`); err == nil {
		t.Fatal("new SQL bypassed membership")
	}
	if _, err = db.pool.Exec(ctx, `UPDATE public.task_repository_scopes SET repository_id='repo_00000000000000000042'`); err == nil {
		t.Fatal("forged scope binding accepted")
	}
	// Repeat migration remains a no-op with the same historical rows.
	if _, err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
}
