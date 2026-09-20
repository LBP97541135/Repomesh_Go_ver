package main

import (
	"context"
	"crypto/rand"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"repomesh.local/repomesh/internal/execution"
	"repomesh.local/repomesh/internal/secrets"
	"repomesh.local/repomesh/internal/testdb"
	"repomesh.local/repomesh/internal/typesafe"
)

func TestReviewScriptUsesIndependentCandidateCopy(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires native unix shell")
	}
	source, review, tools := t.TempDir(), t.TempDir(), t.TempDir()
	repo := filepath.Join(source, "repo")
	if err := os.Mkdir(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	git := func(args ...string) {
		t.Helper()
		c := exec.Command("git", args...)
		c.Dir = repo
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("fixture git failed: %s", out)
		}
	}
	git("init", "-q")
	if err := os.WriteFile(filepath.Join(repo, "business.txt"), []byte("candidate\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", "business.txt")
	git("-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-qm", "candidate")
	// Even an agent that writes to its own copy cannot alter the developer's
	// candidate tree. The review clone must not inherit the source remote.
	stub := "#!/bin/sh\nset -e\ntest -z \"$(git remote)\"\nprintf reviewed > business.txt\nprintf %s \"$*\" > ../received-prompt.txt\n"
	if err := os.WriteFile(filepath.Join(tools, "codex"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	command, err := buildReviewCommand("codex_cli", "MiniMax-M2", "A quoted requirement: \"literal\" $(touch escaped) `false` \\", source)
	if err != nil {
		t.Fatal(err)
	}
	inner := strings.TrimSuffix(strings.TrimPrefix(command, "bash -c '"), "'")
	if strings.Contains(inner, "'") {
		t.Fatal("serialized script has an unescaped quote")
	}
	c := exec.Command("bash", "-c", inner)
	c.Dir = review
	c.Env = append(os.Environ(), "PATH="+tools+":"+os.Getenv("PATH"), "REPOMESH_TYPESAFE_HELPER=")
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("review script failed: %s", out)
	}
	data, _ := os.ReadFile(filepath.Join(repo, "business.txt"))
	if string(data) != "candidate\n" {
		t.Fatal("review changed developer worktree")
	}
	if _, err := os.Stat(filepath.Join(review, "repo", "escaped")); !os.IsNotExist(err) {
		t.Fatal("requirement executed as shell code")
	}
	data, _ = os.ReadFile(filepath.Join(review, "received-prompt.txt"))
	if !strings.Contains(string(data), "$(touch escaped)") {
		t.Fatal("literal requirement was corrupted")
	}
}

func TestTypeSafeSwitchSchedulesReviewAndReviewCannotAdvanceTask(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()
	p := testdb.SeedProject(t, pool, "", "", "fixture/repo")
	testdb.SeedIssue(t, pool, p, "review-issue", "fixture/repo")
	root := make([]byte, 32)
	_, _ = rand.Read(root)
	path := filepath.Join(t.TempDir(), "root")
	if err := os.WriteFile(path, root, 0o600); err != nil {
		t.Fatal(err)
	}
	vault, err := secrets.New(ctx, pool, secrets.Config{ActiveRootID: "review-fixture", Roots: []secrets.RootFile{{ID: "review-fixture", Path: path}}})
	if err != nil {
		t.Fatal(err)
	}
	service := typesafe.New(pool, vault)
	var revision int64
	for _, enabled := range []bool{false, true} {
		settings := typesafe.SaveInput{ExpectedRevision: revision, Enabled: enabled, Model: typesafe.DefaultModel}
		settings.Secret.Mode = "replace"
		settings.Secret.Value = "fixture-review-key"
		v, err := service.Save(ctx, p.OwnerID, p.ID, settings)
		if err != nil {
			t.Fatal(err)
		}
		revision = v.Revision
		var taskID string
		err = pool.QueryRow(ctx, `INSERT INTO public.tasks(id,organization_id,project_id,repository_id,title,instruction,status,source_ref)
			VALUES(gen_random_uuid(),$1,$2,'fixture/repo','Review fixture','Verify candidate','running',jsonb_build_object('issueId','review-issue')) RETURNING id::text`, p.OrganizationID, p.ID).Scan(&taskID)
		if err != nil {
			t.Fatal(err)
		}
		ledger := &coordinatorLedger{pool: pool}
		if _, err := ledger.ReserveForTask(ctx, "review-worker", taskID, "codex_cli", "Review fixture", "Verify candidate"); err != nil {
			t.Fatal(err)
		}
		var reviews int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM repomesh_execution.agent_runs WHERE task_package_ref=$1 AND agent_kind='review_agent'`, taskID).Scan(&reviews); err != nil {
			t.Fatal(err)
		}
		if (reviews == 1) != enabled {
			t.Fatal("unified switch not respected")
		}
		execService := execution.New(pool)
		kinds := []string{"codex_cli", "test_agent"}
		if enabled {
			kinds = append(kinds, "review_agent")
		}
		for _, want := range kinds {
			command, runID, err := execService.ClaimAgentLaunch(ctx, "review-worker")
			if err != nil || command.AgentKind != want {
				t.Fatalf("claim order: got %s want %s (%v)", command.AgentKind, want, err)
			}
			if err := execService.MarkAgentRunning(ctx, runID, 123); err != nil {
				t.Fatal(err)
			}
			code := 0
			if want == "review_agent" {
				code = 1
			}
			if err := execService.MarkAgentExited(ctx, runID, code, false, ""); err != nil {
				t.Fatal(err)
			}
			var status string
			if err := pool.QueryRow(ctx, `SELECT status FROM public.tasks WHERE id=$1::uuid`, taskID).Scan(&status); err != nil {
				t.Fatal(err)
			}
			if status != "blocked" {
				t.Fatalf("observer changed manager gate to %s", status)
			}
		}
	}
}
