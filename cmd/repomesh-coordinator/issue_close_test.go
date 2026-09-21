package main

import (
	"context"
	"testing"

	"repomesh.local/repomesh/internal/testdb"
)

// TestCloseDeliveredIssuesOnlyWhenEverythingMerged 钉住自动收口的边界：
// 只有「有计划 + 任务全 done + change set 全 merged 且至少一条」才关。
//
// 用户报的现象是"都已经合并完成了，为什么没有关闭 issues"；但反过来更危险的是
// **关早了** —— 那会让还没交付完的需求从列表上消失。所以四种"没走完"的情形
// 一条都不许关。
func TestCloseDeliveredIssuesOnlyWhenEverythingMerged(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()

	type fixture struct {
		issueID string
		planID  string
		taskID  string
	}
	seed := func(issueID string, taskStatus string, withPlan bool, changeSetStatus string) fixture {
		t.Helper()
		project := testdb.SeedProject(t, pool, "", "", "fixture/repo")
		testdb.SeedIssue(t, pool, project, issueID, "fixture/repo")
		if _, err := pool.Exec(ctx, `UPDATE repomesh_issues.issues SET hitl_mode='ai' WHERE id=$1`, issueID); err != nil {
			t.Fatal(err)
		}
		if !withPlan {
			return fixture{issueID: issueID}
		}
		var planID string
		if err := pool.QueryRow(ctx, `INSERT INTO public.plans(id, project_id, issue_id)
			VALUES(gen_random_uuid(), $1, $2) RETURNING id::text`, project.ID, issueID).Scan(&planID); err != nil {
			t.Fatal(err)
		}
		var taskID string
		if err := pool.QueryRow(ctx, `INSERT INTO public.tasks(id, organization_id, project_id, repository_id, title, status, plan_id)
			VALUES(gen_random_uuid(), $1, $2, 'fixture/repo', 'fixture task', $3, $4::uuid) RETURNING id::text`,
			project.OrganizationID, project.ID, taskStatus, planID).Scan(&taskID); err != nil {
			t.Fatal(err)
		}
		if changeSetStatus != "" {
			if _, err := pool.Exec(ctx, `INSERT INTO public.change_sets(id, organization_id, task_id, repository_ids, status)
				VALUES(gen_random_uuid(), $1, $2::uuid, '[]'::jsonb, $3)`,
				project.OrganizationID, taskID, changeSetStatus); err != nil {
				t.Fatal(err)
			}
		}
		return fixture{issueID: issueID, planID: planID, taskID: taskID}
	}

	delivered := seed("close-delivered", "done", true, "merged")
	stillRunning := seed("close-running", "running", true, "merged")
	notMerged := seed("close-draft", "done", true, "draft")
	noChangeSet := seed("close-nochangeset", "done", true, "")
	noPlan := seed("close-noplan", "done", false, "")

	closed, err := sweepCloseDeliveredIssues(ctx, pool, nil)
	if err != nil {
		t.Fatal(err)
	}
	if closed != 1 {
		t.Fatalf("closed = %d, want exactly 1（只有交付全部合完的那条该关）", closed)
	}
	for _, c := range []struct {
		name    string
		issueID string
		want    bool // true = 应当已归档
	}{
		{"交付全部合完", delivered.issueID, true},
		{"还有任务在跑", stillRunning.issueID, false},
		{"change set 还没合", notMerged.issueID, false},
		{"一条 change set 都没有", noChangeSet.issueID, false},
		{"还没有计划", noPlan.issueID, false},
	} {
		var archived *string
		if err := pool.QueryRow(ctx, `SELECT archived_at::text FROM repomesh_issues.issues WHERE id=$1`, c.issueID).
			Scan(&archived); err != nil {
			t.Fatal(err)
		}
		if (archived != nil) != c.want {
			t.Fatalf("%s：archived=%v，want %v", c.name, archived != nil, c.want)
		}
	}
}
