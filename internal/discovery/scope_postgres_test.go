package discovery

import (
	"context"
	"errors"
	"testing"

	"repomesh.local/repomesh/internal/testdb"
)

func TestIssueScopeApprovalPlanAndMaterialization(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()
	const issue = "iss_scope_boundary"
	seedRecallFixture(t, ctx, pool, issue, "checkout payment", []string{"checkout", "payment"})
	service := New(pool)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	cards, err := service.loadRepoPool(ctx, tx, "99999999-9999-4999-8999-999999999999", issue)
	tx.Rollback(ctx)
	if err != nil || len(cards) != 2 {
		t.Fatalf("scoped cards=%+v err=%v", cards, err)
	}
	for _, card := range cards {
		if card.Name != "acme/checkout" && card.Name != "acme/shared-lib" {
			t.Fatalf("leaked candidate: %s", card.Name)
		}
	}
	if _, err = service.Candidates(ctx, issue, agentID, "candidates", 20, nil); err != nil {
		t.Fatal(err)
	}
	if _, err = service.Classification(ctx, issue, agentID, "classify"); err != nil {
		t.Fatal(err)
	}
	version := evidenceVersion(t, ctx, pool, issue)
	for _, repo := range []string{"outside/checkout-secret", "acme/docs-site", "invented/repo"} {
		_, err = service.Approval(ctx, issue, agentID, "bad-"+repo, "approved", "", []Adjustment{{Repository: repo, Tier: "required"}}, version)
		if !errors.Is(err, ErrConflict) {
			t.Fatalf("approval %s: %v", repo, err)
		}
	}
	_, err = service.Approval(ctx, issue, agentID, "approve", "approved", "", []Adjustment{{Repository: "acme/shared-lib", Tier: "excluded"}}, version)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.Plan(ctx, issue, agentID, "plan"); err != nil {
		t.Fatal(err)
	}
	plan := readJSONColumn(t, ctx, pool, "plan", issue)
	repos := plan["repositories"].([]any)
	if len(repos) != 1 || repos[0] != "acme/checkout" {
		t.Fatalf("approval excluded repository returned in plan: %v", repos)
	}
	if _, err = service.Approval(ctx, issue, agentID, "approval-expanded", "approved", "", []Adjustment{{Repository: "acme/shared-lib", Tier: "required"}}, version); err != nil {
		t.Fatal(err)
	}
	if _, err = service.Materialize(ctx, issue, agentID, "stale-approval-plan"); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale approval materialized: %v", err)
	}
	if _, err = service.Approval(ctx, issue, agentID, "approval-restored", "approved", "", []Adjustment{{Repository: "acme/shared-lib", Tier: "excluded"}}, version); err != nil {
		t.Fatal(err)
	}

	if _, err = service.Materialize(ctx, issue, agentID, "materialize"); err != nil {
		t.Fatal(err)
	}
	if _, err = service.Materialize(ctx, issue, agentID, "materialize"); err != nil {
		t.Fatalf("replay: %v", err)
	}
	var count int
	var taskID, repoID, scopeIssue, organization string
	if err = pool.QueryRow(ctx, `SELECT t.id::text,s.repository_id,s.issue_id,t.organization_id::text
 FROM public.tasks t JOIN public.task_repository_scopes s ON s.task_id=t.id WHERE s.issue_id=$1`, issue).Scan(&taskID, &repoID, &scopeIssue, &organization); err != nil {
		t.Fatal(err)
	}
	var expectedRepo, expectedOrg string
	if err = pool.QueryRow(ctx, `SELECT r.id,p.organization_id::text FROM repomesh_projects.projects p
 JOIN repomesh_projects.project_repositories pr ON pr.project_id=p.id
 JOIN repomesh_projects.repositories r ON r.id=pr.repository_id WHERE p.id=$1 AND r.name='checkout'`, "99999999-9999-4999-8999-999999999999").Scan(&expectedRepo, &expectedOrg); err != nil {
		t.Fatal(err)
	}
	if repoID != expectedRepo || scopeIssue != issue || organization != expectedOrg {
		t.Fatalf("wrong canonical binding: %s %s %s", repoID, scopeIssue, organization)
	}
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM public.tasks WHERE plan_id=$1`, plan["plan_id"]).Scan(&count); err != nil || count != 1 {
		t.Fatalf("partial/duplicate tasks count=%d err=%v", count, err)
	}
	for _, repo := range []string{"acme/docs-site", "outside/checkout-secret"} {
		if _, err = pool.Exec(ctx, `UPDATE public.tasks SET repository_id=$2 WHERE id=$1`, taskID, repo); err == nil {
			t.Fatalf("direct SQL allowed %s", repo)
		}
	}
	if _, err = pool.Exec(ctx, `UPDATE public.tasks SET plan_id=NULL,source_ref='{}' WHERE id=$1`, taskID); err == nil {
		t.Fatal("issue binding can be erased")
	}
	if _, err = pool.Exec(ctx, `UPDATE repomesh_issues.issue_discoveries SET plan=jsonb_set(plan,'{repositories}','["acme/checkout","acme/docs-site"]') WHERE issue_id=$1`, issue); err != nil {
		t.Fatal(err)
	}
	if _, err = service.Materialize(ctx, issue, agentID, "poisoned-materialize"); !errors.Is(err, ErrConflict) {
		t.Fatalf("tampered historical plan: %v", err)
	}
	if _, err = NewMaintenance(pool).Archive(ctx, issue); err != nil {
		t.Fatal(err)
	}
	if _, err = NewMaintenance(pool).Purge(ctx, issue); !errors.Is(err, ErrConflict) {
		t.Fatalf("purge should preserve execution scope: %v", err)
	}
}
