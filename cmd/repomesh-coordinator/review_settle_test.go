package main

import (
	"context"
	"testing"

	"repomesh.local/repomesh/internal/testdb"
)

// TestSettleDiscoveryMirrors 钉住用户 2026-09-21 报的那个 bug：
// 「仓库范围分档待审批明明已经审核过了，审核台里还一直显示待审」。
//
// 审核台那张单是发现链人工步骤的**镜像登记**（origin='discovery'）。自动托管模式
// 下 ③ 审批 / ⑤ 物化由 coordinator 内部完成，此前那条路从不销单，镜像单于是永远
// 停在 pending。这里按**发现链实际状态**对账，两种 checkpoint 都要销掉。
//
// 同时钉住反面：**还没走到那一步的 issue 一行都不许动** —— 把没审的单标成已审，
// 比留着待审更坏。
func TestSettleDiscoveryMirrors(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()

	project := testdb.SeedProject(t, pool, "", "", "fixture/repo")

	seedDiscovery := func(issueID, approvalState string, materialized bool) {
		t.Helper()
		testdb.SeedIssue(t, pool, project, issueID, "fixture/repo")
		approval := "{}"
		if approvalState != "" {
			approval = `{"state":"` + approvalState + `"}`
		}
		var materialization any
		if materialized {
			materialization = `{"receipt":"materialized"}`
		}
		if _, err := pool.Exec(ctx, `INSERT INTO repomesh_issues.issue_discoveries
			(issue_id, project_id, approval, materialization)
			VALUES($1,$2,$3::jsonb,$4::jsonb)`, issueID, project.ID, approval, materialization); err != nil {
			t.Fatal(err)
		}
	}

	seedReview := func(issueID, checkpoint string) string {
		t.Helper()
		var id string
		if err := pool.QueryRow(ctx, `INSERT INTO public.review_requests
			(id, project_id, object_type, object_id, request_content, status, checkpoint,
			 evidence_version, title, summary)
			VALUES (gen_random_uuid(), $1::uuid, 'project', $1::uuid,
			        jsonb_build_object('origin','discovery','issue_id',$2::text),
			        'pending', $3, 'ev-1', '分档待审批：fixture', '由发现链自动登记')
			RETURNING id::text`, project.ID, issueID, checkpoint).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}

	// ③ 已批 + ⑤ 已物化：两张单都该销。
	approvedIssue := "iss_settle_approved"
	seedDiscovery(approvedIssue, "approved", true)
	scopeID := seedReview(approvedIssue, "repository_scope")
	executionID := seedReview(approvedIssue, "execution")

	// 只批了 ③、还没物化：repository_scope 该销，execution 必须留着。
	halfwayIssue := "iss_settle_halfway"
	seedDiscovery(halfwayIssue, "approved", false)
	halfwayScope := seedReview(halfwayIssue, "repository_scope")
	halfwayExecution := seedReview(halfwayIssue, "execution")

	// 还没走到 ③：一张都不许动。
	untouchedIssue := "iss_settle_untouched"
	seedDiscovery(untouchedIssue, "", false)
	untouchedScope := seedReview(untouchedIssue, "repository_scope")
	untouchedExecution := seedReview(untouchedIssue, "execution")

	// 非 discovery 出处的待审单不归这条对账管（那是审核台自己的卡点）。
	manualID := seedReview(approvedIssue, "repository_scope")
	if _, err := pool.Exec(ctx, `UPDATE public.review_requests
		SET request_content='{"origin":"pipeline"}'::jsonb WHERE id=$1::uuid`, manualID); err != nil {
		t.Fatal(err)
	}

	swept, err := settleDiscoveryMirrors(ctx, pool, "")
	if err != nil {
		t.Fatalf("settle: %v", err)
	}
	if swept != 3 {
		t.Fatalf("应当销 3 张（approved 的 2 张 + halfway 的 repository_scope），实际 %d", swept)
	}

	statusOf := func(id string) string {
		t.Helper()
		var status string
		if err := pool.QueryRow(ctx, `SELECT status FROM public.review_requests WHERE id=$1::uuid`, id).Scan(&status); err != nil {
			t.Fatal(err)
		}
		return status
	}

	if got := statusOf(scopeID); got != "approved" {
		t.Errorf("③ 已批 → repository_scope 镜像单应当是 approved，实际 %q", got)
	}
	if got := statusOf(executionID); got != "approved" {
		t.Errorf("⑤ 已物化 → execution 镜像单应当是 approved，实际 %q", got)
	}
	if got := statusOf(halfwayScope); got != "approved" {
		t.Errorf("③ 已批 → repository_scope 镜像单应当是 approved，实际 %q", got)
	}
	// 反面：没走到的、以及不是 discovery 出处的，一行都不许动。
	if got := statusOf(halfwayExecution); got != "pending" {
		t.Errorf("还没物化 → execution 镜像单必须留在 pending，实际 %q", got)
	}
	if got := statusOf(untouchedScope); got != "pending" {
		t.Errorf("还没审批 → repository_scope 镜像单必须留在 pending，实际 %q", got)
	}
	if got := statusOf(untouchedExecution); got != "pending" {
		t.Errorf("还没审批 → execution 镜像单必须留在 pending，实际 %q", got)
	}
	if got := statusOf(manualID); got != "pending" {
		t.Errorf("origin=pipeline 的待审单不归这条对账管，必须留在 pending，实际 %q", got)
	}

	// 幂等：再跑一次不该再动任何行。
	again, err := settleDiscoveryMirrors(ctx, pool, "")
	if err != nil {
		t.Fatalf("second settle: %v", err)
	}
	if again != 0 {
		t.Fatalf("对账必须幂等，第二次却动了 %d 行", again)
	}

	// 单条 issue 的对账（自动托管那条路走的形态）。
	single, err := settleDiscoveryMirrors(ctx, pool, untouchedIssue)
	if err != nil {
		t.Fatalf("single settle: %v", err)
	}
	if single != 0 {
		t.Fatalf("未推进的 issue 单条对账不该动行，实际 %d", single)
	}
}
