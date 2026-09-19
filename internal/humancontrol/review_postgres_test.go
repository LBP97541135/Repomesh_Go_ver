package humancontrol

import (
	"context"
	"testing"
	"time"

	"repomesh.local/repomesh/internal/testdb"
)

// 真实 Postgres：审核单生产者的四条事实。
//
// 2026-09-19 事故：这张表此前**全仓没有任何生产者**（线上实测 0 行、grep 不到
// 任何 INSERT），而人工审核台读它 —— 于是从上线起就恒空：用户明明在 issue 里
// 看到「待人工」，审核台却永远显示「没有待审事项」。
func TestPostgresReviewRequestMirror(t *testing.T) {
	pool := testdb.Open(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	owner := seedPolicyAccount(t, pool, 773001)
	stranger := seedPolicyAccount(t, pool, 773002)
	projectID := seedPolicyProject(t, pool, owner)
	service := New(pool)

	command := RequestCommand{
		ProjectID:  projectID,
		Checkpoint: "repository_scope",
		Title:      "分档待审批：满减活动",
		Summary:    "由发现链自动登记",
		Assignee:   owner,
		IssueID:    "iss_mirror_fixture",
		Origin:     "discovery",
	}
	first, err := service.Request(ctx, command)
	if err != nil {
		t.Fatalf("Request: %v", err)
	}
	if first.ID == "" {
		t.Fatal("审核单必须拿到 id")
	}
	// ① 幂等：发现链会重放，重放不该堆出一排待审。
	second, err := service.Request(ctx, command)
	if err != nil {
		t.Fatalf("Request（重放）: %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("同键重放应返回同一张单，得到 %s / %s", first.ID, second.ID)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM public.review_requests WHERE project_id=$1::uuid`, projectID).Scan(&count); err != nil {
		t.Fatalf("计数: %v", err)
	}
	if count != 1 {
		t.Fatalf("重放后应只有一张单，得到 %d", count)
	}
	// ② 出处回读：审核台据此指回 issue（而不是给一个推不动流水线的按钮）。
	if first.Origin != "discovery" || first.IssueID != "iss_mirror_fixture" {
		t.Fatalf("出处没回读出来：origin=%q issue=%q", first.Origin, first.IssueID)
	}
	// ③ 可见性：非管理员只看得到指派给自己的那一条。
	mine, err := service.List(ctx, owner, false, "pending")
	if err != nil {
		t.Fatalf("List(owner): %v", err)
	}
	if len(mine) != 1 || mine[0].ID != first.ID {
		t.Fatalf("项目属主应看到自己名下的待审，得到 %+v", mine)
	}
	others, err := service.List(ctx, stranger, false, "pending")
	if err != nil {
		t.Fatalf("List(stranger): %v", err)
	}
	if len(others) != 0 {
		t.Fatalf("别的账号不该看到这条待审，得到 %+v", others)
	}
	// ④ 销账：人工在 issue 页面完成后，这边的待审必须消失，
	//    否则审核台会一直挂着已经做完的事。
	if err := service.ResolveForCheckpoint(ctx, projectID, "repository_scope", owner, "approved", "分档已确认"); err != nil {
		t.Fatalf("ResolveForCheckpoint: %v", err)
	}
	pending, err := service.List(ctx, owner, false, "pending")
	if err != nil {
		t.Fatalf("List(after): %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("销账后不该还有待审，得到 %+v", pending)
	}
	resolved, err := service.List(ctx, owner, false, "approved")
	if err != nil {
		t.Fatalf("List(approved): %v", err)
	}
	if len(resolved) != 1 || resolved[0].ID != first.ID {
		t.Fatalf("已决列表应保留那一条，得到 %+v", resolved)
	}
	// ⑤ 销账之后可以再落一张新的：同一个卡点在不同轮次会再停一次。
	if _, err := service.Request(ctx, command); err != nil {
		t.Fatalf("销账后再登记: %v", err)
	}
	pending, err = service.List(ctx, owner, false, "pending")
	if err != nil {
		t.Fatalf("List(again): %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("销账后应能再登记一张，得到 %+v", pending)
	}
}
