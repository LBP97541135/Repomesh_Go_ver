package tasks

import (
	"context"
	"errors"
	"testing"

	"repomesh.local/repomesh/internal/testdb"
)

func TestPlanAndRevisionStayInsideProjectAndIssue(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()
	f := testdb.SeedProject(t, pool, "", "", "owner/a", "owner/b")
	outside := testdb.SeedProject(t, pool, "", "", "outsider/c")
	testdb.SeedIssue(t, pool, f, "iss_plan_scope", "owner/a")
	store := NewPostgresStore(pool)
	if _, err := store.CreatePlan(ctx, PlanWrite{ProjectID: f.ID, Batches: [][]string{{"outsider/c"}}}); !errors.Is(err, ErrInvalidPlan) {
		t.Fatalf("cross-project plan: %v", err)
	}
	plan, err := store.CreatePlan(ctx, PlanWrite{ProjectID: f.ID, Batches: [][]string{{"owner/a"}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `UPDATE public.plans SET issue_id='iss_plan_scope' WHERE id=$1`, plan.ID); err != nil {
		t.Fatal(err)
	}
	for _, repo := range []string{"owner/b", "outsider/c"} {
		_, err = store.ApplyRevision(ctx, RevisionCommand{PlanID: plan.ID, ExpectedVersion: "v1", NewBatches: [][]string{{"owner/a", repo}}, NewTasks: []TaskSnapshot{{TaskUID: "bad", RepositoryID: repo, Title: "Out of scope"}}, Actor: "leader", Reason: "test", IdempotencyKey: "bad-" + repo})
		if !errors.Is(err, ErrInvalidPlan) {
			t.Fatalf("revision %s: %v", repo, err)
		}
	}
	// A forged task cannot use a valid project but a repository from another project.
	_, err = store.CreateTask(ctx, TaskWrite{OrganizationID: f.OrganizationID, ProjectID: f.ID, PlanID: plan.ID, RepositoryID: outside.Repositories["outsider/c"], Title: "Forged", IdempotencyKey: "forged"})
	if err == nil {
		t.Fatal("CreateTask allowed cross-project repository")
	}
	_, err = store.ApplyRevision(ctx, RevisionCommand{PlanID: plan.ID, ExpectedVersion: "v1", NewBatches: [][]string{{"owner/a"}}, NewTasks: []TaskSnapshot{{TaskUID: "valid", RepositoryID: "owner/a", Title: "Valid"}}, Actor: "leader", Reason: "test", IdempotencyKey: "valid"})
	if err != nil {
		t.Fatal(err)
	}
	var tasks int
	var issue string
	if err = pool.QueryRow(ctx, `SELECT count(*),min(issue_id) FROM public.task_repository_scopes WHERE project_id=$1`, f.ID).Scan(&tasks, &issue); err != nil || tasks != 1 || issue != "iss_plan_scope" {
		t.Fatalf("atomic revision/binding: tasks=%d issue=%s err=%v", tasks, issue, err)
	}
	var version string
	if err = pool.QueryRow(ctx, `SELECT plan_version FROM public.plans WHERE id=$1`, plan.ID).Scan(&version); err != nil || version != "v2" {
		t.Fatalf("rejected revision advanced plan: %s %v", version, err)
	}
}
