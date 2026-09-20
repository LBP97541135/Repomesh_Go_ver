//go:build unix

package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"repomesh.local/repomesh/internal/execution"
	"repomesh.local/repomesh/internal/testdb"
)

func TestTestEvidenceRequiresBothActualAndReportedSuccess(t *testing.T) {
	yes, no, zero, one := true, false, 0, 1
	for _, c := range []struct {
		e    testEvidence
		exit int
		want bool
	}{
		{testEvidence{Passed: &yes, ExitCode: &zero}, 0, true},
		{testEvidence{Passed: &yes, ExitCode: &one}, 0, false},
		{testEvidence{Passed: &yes, ExitCode: &zero}, 1, false},
		{testEvidence{Passed: &yes}, 0, false},
		{testEvidence{Passed: &no, ExitCode: &zero}, 0, false},
	} {
		if got := testEvidencePassed(c.e, c.exit); got != c.want {
			t.Fatal("test verdict ignored a failing or missing fact")
		}
	}
}

func TestIntegrationReferencePersistsTheRealSixPartReference(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()
	p := testdb.SeedProject(t, pool, "", "", "fixture/repo")
	testdb.SeedIssue(t, pool, p, "integration-issue", "fixture/repo")
	var plan string
	if err := pool.QueryRow(ctx, `INSERT INTO public.plans(id,project_id,issue_id) VALUES(gen_random_uuid(),$1,'integration-issue') RETURNING id::text`, p.ID).Scan(&plan); err != nil {
		t.Fatal(err)
	}
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, execution.TestEvidenceFile), []byte(`{"command":"fixture test","exit_code":0,"passed":true,"summary":"synthetic evidence"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	e := &executor{pool: pool}
	e.recordIntegrationEvidence(ctx, "integration-run", "plan:"+plan+":kind:repo_integration:repo:fixture/repo", workspace, 0)
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM public.test_evidence WHERE plan_id=$1::uuid AND passed AND kind='repo_integration'`, plan).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatal("valid integration reference dropped its evidence")
	}
}

func TestSanitizedEnvironmentDoesNotInheritProviderOrRunKeys(t *testing.T) {
	t.Setenv("TYPESAFE_API_KEY", "fixture-private-key")
	t.Setenv("REPOMESH_TYPESAFE_GRANT", "old-grant")
	for _, entry := range sanitizedEnv("") {
		if entry == "TYPESAFE_API_KEY=fixture-private-key" || entry == "REPOMESH_TYPESAFE_GRANT=old-grant" {
			t.Fatal("inherited another process credential")
		}
	}
}

func TestDeliveryCIFailsWhenTestEvidenceContradictsAgentExit(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()
	p := testdb.SeedProject(t, pool, "", "", "fixture/repo")
	testdb.SeedIssue(t, pool, p, "ci-issue", "fixture/repo")
	var taskID string
	if err := pool.QueryRow(ctx, `INSERT INTO public.tasks(id,organization_id,project_id,repository_id,source_ref)
		VALUES(gen_random_uuid(),$1,$2,'fixture/repo',jsonb_build_object('issueId','ci-issue')) RETURNING id::text`, p.OrganizationID, p.ID).Scan(&taskID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO repomesh_execution.workers(id,host,kind) VALUES('ci-worker','fixture','host_executor')`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO repomesh_execution.attempts(id,project_id,issue_id,worker_id,configuration_revision,state)
		VALUES('ci-attempt',$1,'ci-issue','ci-worker',$2,'running')`, p.ID, p.ConfigurationID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO repomesh_execution.agent_runs(id,attempt_id,agent_kind,command,workspace,task_package_ref,state,exit_code,exited_at)
		VALUES('ci-run','ci-attempt','test_agent','fixture','/fixture',$1,'exited',0,now())`, taskID); err != nil {
		t.Fatal(err)
	}
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, execution.TestEvidenceFile), []byte(`{"command":"fixture test","exit_code":1,"passed":true,"summary":"inconsistent agent assertion"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	e := &executor{pool: pool}
	e.recordDeliveryFacts(ctx, "ci-run", workspace)
	var status string
	if err := pool.QueryRow(ctx, `SELECT c.status FROM public.scm_commands c JOIN public.change_sets s ON s.id=c.change_set_id WHERE s.task_id=$1::uuid AND c.command_type='ci'`, taskID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "failed" {
		t.Fatal("delivery gate accepted a failed test as successful CI")
	}
	var passed bool
	if err := pool.QueryRow(ctx, `SELECT passed FROM public.test_evidence WHERE task_id=$1::uuid`, taskID).Scan(&passed); err != nil {
		t.Fatal(err)
	}
	if passed {
		t.Fatal("displayed test result contradicts CI gate")
	}
}
