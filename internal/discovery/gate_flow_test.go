package discovery

import (
	"context"
	"testing"
	"time"

	"repomesh.local/repomesh/internal/testdb"
)

// 选仓门开门时机(spec 2026-09-20 修订版,Task 顺序修正):① 需求分析产物
// 落库的那一刻**开门**(建议为空;ai 模式带 10 分钟截止,hitl 无截止);② 候选产物
// 落库只是**补建议**,门不因候选落库才出现/消失。重复开门幂等。
func TestApplyPlanningAnalysisOpensGateAndCandidatesFillSuggestion(t *testing.T) {
	pool := testdb.Open(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	seed := func(issueID, hitlMode string) {
		t.Helper()
		fixture := testdb.SeedProject(t, pool, "", "", "acme/checkout", "acme/shared-lib", "acme/docs-site")
		testdb.SeedIssue(t, pool, fixture, issueID, "acme/checkout", "acme/shared-lib")
		if hitlMode != "" {
			exec := func(sql string, args ...any) {
				t.Helper()
				if _, err := pool.Exec(ctx, sql, args...); err != nil {
					t.Fatalf("fixture: %v", err)
				}
			}
			exec(`UPDATE repomesh_issues.issues SET hitl_mode=$2 WHERE id=$1`, issueID, hitlMode)
		}
	}
	analysisArtifact := func() map[string]any {
		return map[string]any{
			"dimensions":           []any{map[string]any{"name": "业务场景", "covered": true, "note": "改结算"}},
			"questions":            []any{},
			"extracted_keywords":   []any{"结算"},
			"analyzed_requirement": "改造结算流程",
		}
	}
	candidatesArtifact := func(names ...string) map[string]any {
		entries := []any{}
		for _, name := range names {
			entries = append(entries, map[string]any{
				"repository": name, "tier": "required", "score": 0.9, "reason": "需求点名了 " + name,
			})
		}
		return map[string]any{"candidates": entries}
	}
	prov := PlanningProvenance{Role: "organization_leader", SkillID: "cross-repo-planning", RunID: "run_gate_b1"}
	service := New(pool)

	// ai 模式:① 分析落库即开门,截止 = 分析时刻 + 10 分钟,建议为空。
	const aiIssue = "iss_gate_open_ai"
	seed(aiIssue, "ai")
	before := time.Now().UTC()
	if err := service.ApplyPlanningRun(ctx, aiIssue, PlanningAnalysis, analysisArtifact(), prov); err != nil {
		t.Fatalf("ApplyPlanningRun analysis: %v", err)
	}
	after := time.Now().UTC()
	gate, err := service.Gate(ctx, aiIssue)
	if err != nil {
		t.Fatalf("Gate: %v", err)
	}
	if gate == nil || gate.State != GatePending {
		t.Fatalf("ai 模式分析落库应开 pending 门: %+v", gate)
	}
	if len(gate.Suggested) != 0 {
		t.Fatalf("开门时建议应为空: %+v", gate.Suggested)
	}
	if gate.AIRequested {
		t.Fatalf("开门时 ai_requested 应为 false: %+v", gate)
	}
	if gate.DeadlineAt == nil {
		t.Fatal("ai 模式门必须带截止(10 分钟)")
	}
	if gate.DeadlineAt.Before(before.Add(10*time.Minute)) || gate.DeadlineAt.After(after.Add(10*time.Minute)) {
		t.Fatalf("截止应落在 [开门前+10m, 开门后+10m]: %v", gate.DeadlineAt)
	}
	openedDeadline := *gate.DeadlineAt

	// ② 候选落库 → 只补建议,门与截止不动(不因候选落库而换代)。
	if err := service.ApplyPlanningRun(ctx, aiIssue, PlanningCandidates,
		candidatesArtifact("acme/checkout", "acme/shared-lib"), prov); err != nil {
		t.Fatalf("ApplyPlanningRun candidates: %v", err)
	}
	gate, err = service.Gate(ctx, aiIssue)
	if err != nil {
		t.Fatalf("Gate: %v", err)
	}
	if gate == nil || gate.State != GatePending {
		t.Fatalf("候选落库后门应仍 pending: %+v", gate)
	}
	if len(gate.Suggested) != 2 || gate.Suggested[0] != "acme/checkout" || gate.Suggested[1] != "acme/shared-lib" {
		t.Fatalf("候选落库应补建议全名: %+v", gate.Suggested)
	}
	if gate.DeadlineAt == nil || !gate.DeadlineAt.Equal(openedDeadline) {
		t.Fatalf("候选落库不得改写开门的截止: %v 期望 %v", gate.DeadlineAt, openedDeadline)
	}

	// hitl 模式:分析落库即开门、无截止;候选落库补建议,门仍在。
	const hitlIssue = "iss_gate_open_hitl"
	seed(hitlIssue, "")
	if err := service.ApplyPlanningRun(ctx, hitlIssue, PlanningAnalysis, analysisArtifact(), prov); err != nil {
		t.Fatalf("ApplyPlanningRun analysis: %v", err)
	}
	gate, err = service.Gate(ctx, hitlIssue)
	if err != nil {
		t.Fatalf("Gate: %v", err)
	}
	if gate == nil || gate.State != GatePending || gate.DeadlineAt != nil {
		t.Fatalf("hitl 模式门应 pending 且无截止: %+v", gate)
	}
	if err := service.ApplyPlanningRun(ctx, hitlIssue, PlanningCandidates,
		candidatesArtifact("acme/checkout"), prov); err != nil {
		t.Fatalf("ApplyPlanningRun candidates: %v", err)
	}
	gate, err = service.Gate(ctx, hitlIssue)
	if err != nil {
		t.Fatalf("Gate: %v", err)
	}
	if gate == nil || gate.State != GatePending || gate.DeadlineAt != nil {
		t.Fatalf("候选落库后 hitl 门应仍 pending 且无截止: %+v", gate)
	}
	if len(gate.Suggested) != 1 || gate.Suggested[0] != "acme/checkout" {
		t.Fatalf("候选落库应补建议: %+v", gate.Suggested)
	}

	// 重复应用候选产物:门仍在,建议按最新候选刷新(不再开门、不消失)。
	if err := service.ApplyPlanningRun(ctx, hitlIssue, PlanningCandidates,
		candidatesArtifact("acme/docs-site"), prov); err != nil {
		t.Fatalf("重复 ApplyPlanningRun: %v", err)
	}
	gate, err = service.Gate(ctx, hitlIssue)
	if err != nil {
		t.Fatalf("Gate: %v", err)
	}
	if gate == nil || gate.State != GatePending || gate.DeadlineAt != nil {
		t.Fatalf("重复应用后门状态不应变化: %+v", gate)
	}
	if len(gate.Suggested) != 1 || gate.Suggested[0] != "acme/docs-site" {
		t.Fatalf("重复应用应把建议刷成最新候选: %+v", gate.Suggested)
	}
}

// 门 CAS 写入(FillGateSuggested / MarkGateAIRequested)绝不复活已决的门:
// resolved 的门既不被补建议、也不被置 ai_requested。
func TestGateSingleColumnWritesSkipResolved(t *testing.T) {
	pool := testdb.Open(t)
	ctx := t.Context()
	const issueID = "iss_gate_cas_resolved"
	fixture := testdb.SeedProject(t, pool, "", "", "acme/checkout", "acme/shared-lib")
	testdb.SeedIssue(t, pool, fixture, issueID, "acme/checkout")
	service := New(pool)
	if err := service.OpenGate(ctx, issueID, nil, nil); err != nil {
		t.Fatalf("open gate: %v", err)
	}
	// 点「让 AI 定」→ ai_requested;补建议 → suggested 落下。
	if flipped, err := service.MarkGateAIRequested(ctx, issueID); err != nil || !flipped {
		t.Fatalf("pending 门应置上 ai_requested: %v %v", flipped, err)
	}
	gate, err := service.Gate(ctx, issueID)
	if err != nil || gate == nil || !gate.AIRequested {
		t.Fatalf("ai_requested 未落库: %+v %v", gate, err)
	}
	if err := service.FillGateSuggested(ctx, issueID, []string{"acme/checkout"}); err != nil {
		t.Fatalf("补建议: %v", err)
	}
	// 关门。
	if _, err := service.ResolveGate(ctx, issueID, "ai"); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	// resolved 后:再补建议/再置 ai_requested 都必须无效(不复活)。
	if err := service.FillGateSuggested(ctx, issueID, []string{"acme/shared-lib"}); err != nil {
		t.Fatalf("resolved 后补建议应静默跳过: %v", err)
	}
	if flipped, err := service.MarkGateAIRequested(ctx, issueID); err != nil || flipped {
		t.Fatalf("resolved 门不得再置 ai_requested: %v %v", flipped, err)
	}
	gate, err = service.Gate(ctx, issueID)
	if err != nil || gate == nil {
		t.Fatalf("Gate: %+v %v", gate, err)
	}
	if gate.State != GateResolved || gate.DecidedBy != "ai" {
		t.Fatalf("门应保持 resolved(ai): %+v", gate)
	}
	if !gate.AIRequested {
		t.Fatalf("resolved 后应保留 ai_requested 作为历史事实: %+v", gate)
	}
	if len(gate.Suggested) != 1 || gate.Suggested[0] != "acme/checkout" {
		t.Fatalf("resolved 后建议不得被改写: %+v", gate.Suggested)
	}
}

// 门超时代选(Task B2 的 discovery 侧,A3 服务层等价物):到期后 CAS 置 timeout、
// 建议集合双表落范围、整组同一把 scope_revision;同键重放 200 不重写;
// 门已 resolved 时换新键也不得重写范围。
func TestResolveGateTimeoutReplayAndNoop(t *testing.T) {
	pool := testdb.Open(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	const issueID = "iss_gate_timeout_idem"
	fixture := testdb.SeedProject(t, pool, "", "", "acme/checkout", "acme/shared-lib")
	testdb.SeedIssue(t, pool, fixture, issueID, "acme/checkout")
	service := New(pool)
	past := time.Now().UTC().Add(-time.Minute)
	if err := service.OpenGate(ctx, issueID, []string{"acme/checkout", "acme/shared-lib", "acme/not-in-project"}, &past); err != nil {
		t.Fatalf("open gate: %v", err)
	}
	const key = "autohost:" + issueID + ":gate-timeout"
	receipt, err := service.ResolveGateTimeout(ctx, issueID, key)
	if err != nil {
		t.Fatalf("ResolveGateTimeout: %v", err)
	}
	if receipt.Status != "committed" || receipt.RepositoryCount != 2 {
		t.Fatalf("应 committed 且只落项目内 2 仓: %+v", receipt)
	}
	gate, err := service.Gate(ctx, issueID)
	if err != nil || gate == nil || gate.State != GateResolved || gate.DecidedBy != "timeout" {
		t.Fatalf("超时应 CAS 置 resolved(timeout): %+v %v", gate, err)
	}
	var revision string
	if err := pool.QueryRow(ctx, `SELECT scope_revision FROM repomesh_issues.issue_repository_scope
		WHERE issue_id=$1 AND repository_id=$2`, issueID, fixture.Repositories["acme/checkout"]).Scan(&revision); err != nil {
		t.Fatalf("读范围: %v", err)
	}
	// 同键重放:status=replayed,范围 revision 原样。
	replay, err := service.ResolveGateTimeout(ctx, issueID, key)
	if err != nil || replay.Status != "replayed" || replay.RepositoryCount != 2 {
		t.Fatalf("同键重放应 replayed(2): %+v %v", replay, err)
	}
	var after string
	if err := pool.QueryRow(ctx, `SELECT scope_revision FROM repomesh_issues.issue_repository_scope
		WHERE issue_id=$1 AND repository_id=$2`, issueID, fixture.Repositories["acme/checkout"]).Scan(&after); err != nil || after != revision {
		t.Fatalf("重放不得重写范围 revision: %q 期望 %q %v", after, revision, err)
	}
	// 门已 resolved:换新键也是 noop,不重写。
	noop, err := service.ResolveGateTimeout(ctx, issueID, key+"-again")
	if err != nil || noop.Status != "noop" {
		t.Fatalf("resolved 门换新键应 noop: %+v %v", noop, err)
	}
	if err := pool.QueryRow(ctx, `SELECT scope_revision FROM repomesh_issues.issue_repository_scope
		WHERE issue_id=$1 AND repository_id=$2`, issueID, fixture.Repositories["acme/checkout"]).Scan(&after); err != nil || after != revision {
		t.Fatalf("noop 不得重写范围 revision: %q 期望 %q %v", after, revision, err)
	}
}
