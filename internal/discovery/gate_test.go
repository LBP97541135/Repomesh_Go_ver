package discovery

import (
	"testing"
	"time"

	"repomesh.local/repomesh/internal/testdb"
)

// 选仓门状态机(spec 2026-09-20 §2/§3.2):门是 issue_discoveries 上的独立
// jsonb 列,写入只走单列 CAS,绝不进 save()。这里覆盖三件事:开门幂等
// (重复开不覆盖)、CAS 翻转(非 pending 拒绝翻转)、老 issue(无列值)读 nil。
func TestScopeGateOpenResolveRead(t *testing.T) {
	pool := testdb.Open(t)
	issue := "iss_scope_gate_" + t.Name()
	seedRecallFixture(t, t.Context(), pool, issue, "选仓门", []string{"gate"})
	service := New(pool)
	ctx := t.Context()

	// 老 issue 语义:scope_gate 为空 → nil,调用方视为已 resolve,跳过门。
	gate, err := service.Gate(ctx, issue)
	if err != nil || gate != nil {
		t.Fatalf("空缺门应读 nil,得到 %+v err=%v", gate, err)
	}

	// ai 模式开门:带 10 分钟截止;建议集合原样落 suggested。
	deadline := time.Now().UTC().Add(10 * time.Minute)
	if err := service.OpenGate(ctx, issue, []string{"acme/checkout", "acme/shared-lib"}, &deadline); err != nil {
		t.Fatalf("开门失败: %v", err)
	}
	gate, err = service.Gate(ctx, issue)
	if err != nil || gate == nil || gate.State != GatePending {
		t.Fatalf("开门后应 pending: %+v err=%v", gate, err)
	}
	if len(gate.Suggested) != 2 || gate.Suggested[0] != "acme/checkout" {
		t.Fatalf("建议集合未落库: %+v", gate.Suggested)
	}
	if gate.DeadlineAt == nil || !gate.DeadlineAt.Equal(deadline) {
		t.Fatalf("截止未落库: %v 期望 %v", gate.DeadlineAt, deadline)
	}
	if gate.DecidedBy != "" || gate.ResolvedAt != nil {
		t.Fatalf("pending 门不该有决定人/决定时刻: %+v", gate)
	}

	// 幂等:第二次开门(不同建议、不同截止)不得覆盖已开的门。
	later := deadline.Add(5 * time.Minute)
	if err := service.OpenGate(ctx, issue, []string{"acme/docs-site"}, &later); err != nil {
		t.Fatalf("重复开门报错: %v", err)
	}
	gate, err = service.Gate(ctx, issue)
	if err != nil || gate == nil || len(gate.Suggested) != 2 || !gate.DeadlineAt.Equal(deadline) {
		t.Fatalf("重复开门覆盖了原门: %+v err=%v", gate, err)
	}

	// CAS 翻转:pending → resolved;再翻一次返回 false,decided_by 不被改写。
	flipped, err := service.ResolveGate(ctx, issue, "manual")
	if err != nil || !flipped {
		t.Fatalf("pending 门应可确认: %v %v", flipped, err)
	}
	gate, err = service.Gate(ctx, issue)
	if err != nil || gate == nil || gate.State != GateResolved || gate.DecidedBy != "manual" {
		t.Fatalf("确认后应 resolved(manual): %+v err=%v", gate, err)
	}
	if gate.ResolvedAt == nil {
		t.Fatal("确认应落 resolved_at")
	}
	if len(gate.Suggested) != 2 || gate.DeadlineAt == nil {
		t.Fatalf("确认不得抹掉建议与截止: %+v", gate)
	}
	flipped, err = service.ResolveGate(ctx, issue, "timeout")
	if err != nil || flipped {
		t.Fatalf("非 pending 门不得再翻转: %v %v", flipped, err)
	}
	gate, _ = service.Gate(ctx, issue)
	if gate.DecidedBy != "manual" {
		t.Fatalf("重复确认改写了决定人: %+v", gate)
	}
}

// 开门遇到还没有发现链行的 issue:按 issue 现场补一行最小状态(与
// ensureState 同款思路)——门不依赖"① 已经跑过"。hitl 模式 deadline 为 nil。
func TestScopeGateOpenCreatesMissingDiscoveryRow(t *testing.T) {
	pool := testdb.Open(t)
	f := testdb.SeedProject(t, pool, "", "", "acme/checkout")
	// fixture 的建项聚合触发器要求 issue 至少带一个仓(creation_aggregate_complete);
	// 这里测的是"发现链行缺失时开门补行",与 scope 行无关。
	testdb.SeedIssue(t, pool, f, "iss_scope_gate_fresh", "acme/checkout")
	service := New(pool)
	ctx := t.Context()
	if err := service.OpenGate(ctx, "iss_scope_gate_fresh", []string{"acme/checkout"}, nil); err != nil {
		t.Fatalf("缺行的 issue 开门失败: %v", err)
	}
	gate, err := service.Gate(ctx, "iss_scope_gate_fresh")
	if err != nil || gate == nil || gate.State != GatePending || gate.DeadlineAt != nil {
		t.Fatalf("hitl 门应 pending 且无截止: %+v err=%v", gate, err)
	}
	// issue 不存在:如实拒绝,不静默假装开了门。
	if err := service.OpenGate(ctx, "iss_scope_gate_missing", nil, nil); err == nil {
		t.Fatal("不存在的 issue 开门应报错")
	}
}
