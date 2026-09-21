package main

import (
	"context"
	"testing"
	"time"

	"repomesh.local/repomesh/internal/discovery"
	"repomesh.local/repomesh/internal/testdb"
)

// pendingIssues 的门谓词(Task B2,SQL 级):
//   - state=pending 且有截止且未到期 → 连 tick 都不捡(WHERE 直接排除);
//   - 到期 → 入选且 gateExpired;
//   - resolved → 走原路(正常步进,不撞门分支);
//   - pending 无截止(hitl 语义的门)→ 入选且 gatePending,switch 等待分支兜住。
func TestPendingIssuesGatePredicate(t *testing.T) {
	pool := testdb.Open(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("fixture: %v\nSQL: %s", err, sql)
		}
	}
	seed := func(issueID string) {
		t.Helper()
		fixture := testdb.SeedProject(t, pool, "", "", "acme/checkout", "acme/shared-lib")
		testdb.SeedIssue(t, pool, fixture, issueID, "acme/checkout")
		exec(`UPDATE repomesh_issues.issues SET hitl_mode='ai' WHERE id=$1`, issueID)
	}
	service := discovery.New(pool)
	const waitID = "iss_gate_sql_wait"
	const expireID = "iss_gate_sql_expire"
	const resolvedID = "iss_gate_sql_resolved"
	const nodeadlineID = "iss_gate_sql_nodeadline"
	seed(waitID)
	seed(expireID)
	seed(resolvedID)
	seed(nodeadlineID)
	future := time.Now().UTC().Add(10 * time.Minute)
	if err := service.OpenGate(ctx, waitID, []string{"acme/checkout"}, &future); err != nil {
		t.Fatalf("open gate: %v", err)
	}
	if err := service.OpenGate(ctx, expireID, []string{"acme/checkout"}, &future); err != nil {
		t.Fatalf("open gate: %v", err)
	}
	exec(`UPDATE repomesh_issues.issue_discoveries
		SET scope_gate = jsonb_set(scope_gate, '{deadline_at}', to_jsonb((now() - interval '1 minute')::timestamptz))
		WHERE issue_id=$1`, expireID)
	if err := service.OpenGate(ctx, resolvedID, []string{"acme/checkout"}, &future); err != nil {
		t.Fatalf("open gate: %v", err)
	}
	if _, err := service.ResolveGate(ctx, resolvedID, "manual"); err != nil {
		t.Fatalf("resolve gate: %v", err)
	}
	if err := service.OpenGate(ctx, nodeadlineID, []string{"acme/checkout"}, nil); err != nil {
		t.Fatalf("open gate: %v", err)
	}

	automator := newDiscoveryAutomator(service, pool, nil)
	pending, err := automator.pendingIssues(ctx)
	if err != nil {
		t.Fatalf("pendingIssues: %v", err)
	}
	byID := map[string]discoveryProgress{}
	for _, p := range pending {
		byID[p.issueID] = p
	}
	if _, picked := byID[waitID]; picked {
		t.Fatal("未到期的 pending 门不应入选(连 tick 都不捡)")
	}
	expired, picked := byID[expireID]
	if !picked {
		t.Fatal("到期的门应入选(超时代选)")
	}
	if !expired.gateExpired || expired.gatePending {
		t.Fatalf("到期门应 gateExpired 且非 gatePending: %+v", expired)
	}
	resolved, picked := byID[resolvedID]
	if !picked {
		t.Fatal("resolved 的门应走原路入选")
	}
	if resolved.gatePending || resolved.gateExpired {
		t.Fatalf("resolved 门不应再撞门分支: %+v", resolved)
	}
	if step := autohostStep(resolved); step != 1 {
		t.Fatalf("resolved 门后应从①需求分析起步,得到步 %d", step)
	}
	noDeadline, picked := byID[nodeadlineID]
	if !picked || !noDeadline.gatePending || noDeadline.gateExpired {
		t.Fatalf("无截止的 pending 门应入选且 gatePending(等待分支兜住): picked=%v %+v", picked, noDeadline)
	}
}

// 门等待分支(无截止的门被捡到):return false,**不经 done()**,不进熔断计数。
func TestAutomatorGateWaitDoesNotCount(t *testing.T) {
	pool := testdb.Open(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	const issueID = "iss_gate_wait_flow"
	fixture := testdb.SeedProject(t, pool, "", "", "acme/checkout")
	testdb.SeedIssue(t, pool, fixture, issueID, "acme/checkout")
	if _, err := pool.Exec(ctx, `UPDATE repomesh_issues.issues SET hitl_mode='ai' WHERE id=$1`, issueID); err != nil {
		t.Fatalf("fixture: %v", err)
	}
	// ①② 已完成(门在②产物落库后开),当前唯一可走的分支就是门等待。
	if _, err := pool.Exec(ctx, `INSERT INTO repomesh_issues.issue_discoveries
		(issue_id, project_id, requirement_text, analysis, candidates, idempotency_ledger)
		VALUES ($1, $2, '改结算', '{"sufficient":true,"analyzed_requirement":"改结算"}'::jsonb,
		'{"items":[{"repository_name":"acme/checkout","score":0.9,"agent_tier":"required","rationale":"点名"}],"llm_used":true}'::jsonb,
		'{}'::jsonb)`, issueID, fixture.ID); err != nil {
		t.Fatalf("fixture: %v", err)
	}
	if err := discovery.New(pool).OpenGate(ctx, issueID, []string{"acme/checkout"}, nil); err != nil {
		t.Fatalf("open gate: %v", err)
	}
	automator := newDiscoveryAutomator(discovery.New(pool), pool, nil)
	if automator.step(ctx) {
		t.Fatal("issue 处于门等待时,step 应返回 false(没做任何工作)")
	}
	if len(automator.backoff) != 0 || len(automator.attempts) != 0 {
		t.Fatalf("门等待不得进熔断/退避计数: backoff=%v attempts=%v", automator.backoff, automator.attempts)
	}
}

// 查漏派发(Task B3,spec §3.2):门被**人工**确认(decided_by=manual)后、③ 分档前,
// automator 先派一次 PlanningGapAudit;查漏结论落 gate.audit.missing 后不再派。
// ai/timeout 确认的门不跑查漏(直接进 ③)——这里的门是 ai 模式 issue 上被人在
// 超时前手动确认的情形,所以仍由自动托管循环推进。
func TestAutomatorDispatchesGapAuditAfterManualGate(t *testing.T) {
	pool := testdb.Open(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	const issueID = "iss_gap_audit_dispatch"
	fixture := testdb.SeedProject(t, pool, "", "", "acme/checkout", "acme/shared-lib")
	testdb.SeedIssue(t, pool, fixture, issueID, "acme/checkout")
	if _, err := pool.Exec(ctx, `UPDATE repomesh_issues.issues SET hitl_mode='ai' WHERE id=$1`, issueID); err != nil {
		t.Fatalf("fixture: %v", err)
	}
	// ①② 已完成(分析足够 + 候选在库),门被人工确认 → 下一拍应派查漏而非直接分档。
	if _, err := pool.Exec(ctx, `INSERT INTO repomesh_issues.issue_discoveries
		(issue_id, project_id, requirement_text, analysis, candidates, idempotency_ledger)
		VALUES ($1, $2, '改结算', '{"sufficient":true,"analyzed_requirement":"改结算"}'::jsonb,
		'{"items":[{"repository_name":"acme/checkout","score":0.9,"agent_tier":"required","rationale":"点名"}],"llm_used":true}'::jsonb,
		'{}'::jsonb)`, issueID, fixture.ID); err != nil {
		t.Fatalf("fixture: %v", err)
	}
	service := discovery.New(pool)
	if err := service.OpenGate(ctx, issueID, []string{"acme/checkout"}, nil); err != nil {
		t.Fatalf("open gate: %v", err)
	}
	if _, err := service.ResolveGate(ctx, issueID, "manual"); err != nil {
		t.Fatalf("resolve gate: %v", err)
	}

	automator := newDiscoveryAutomator(service, pool, nil)
	pending, err := automator.pendingIssues(ctx)
	if err != nil {
		t.Fatalf("pendingIssues: %v", err)
	}
	var progress discoveryProgress
	found := false
	for _, p := range pending {
		if p.issueID == issueID {
			progress, found = p, true
		}
	}
	if !found {
		t.Fatal("人工确认的门 issue 应入选自动托管")
	}
	if !progress.gateManual || progress.gapAuditRecorded {
		t.Fatalf("人工确认且未查漏: gateManual=%v gapAuditRecorded=%v", progress.gateManual, progress.gapAuditRecorded)
	}
	if !automator.step(ctx) {
		t.Fatal("人工确认的门应派一次查漏")
	}
	var step int
	if err := pool.QueryRow(ctx, `SELECT step FROM repomesh_issues.planning_runs
		WHERE issue_id=$1 ORDER BY created_at DESC LIMIT 1`, issueID).Scan(&step); err != nil {
		t.Fatalf("读 planning_runs: %v", err)
	}
	if step != discovery.PlanningGapAudit {
		t.Fatalf("应派 PlanningGapAudit(步 %d),得到 %d", discovery.PlanningGapAudit, step)
	}

	// 查漏结论落 gate.audit.missing 后,不再重复派查漏。
	if _, err := pool.Exec(ctx, `UPDATE repomesh_issues.issue_discoveries
		SET scope_gate = jsonb_set(scope_gate, '{audit}',
		       jsonb_set(COALESCE(scope_gate->'audit', '{}'::jsonb), '{missing}', '[]'::jsonb, true), true)
		WHERE issue_id=$1`, issueID); err != nil {
		t.Fatalf("fixture 写查漏结论: %v", err)
	}
	pending, err = newDiscoveryAutomator(service, pool, nil).pendingIssues(ctx)
	if err != nil {
		t.Fatalf("pendingIssues: %v", err)
	}
	found = false
	for _, p := range pending {
		if p.issueID == issueID {
			progress, found = p, true
		}
	}
	if !found || !progress.gapAuditRecorded {
		t.Fatalf("查漏结论落库后应标记已查漏: found=%v %+v", found, progress)
	}
	if got := autohostStep(progress); got != 7 {
		t.Fatalf("已查漏后应进 ③ 分档(步 7),得到 %d", got)
	}
}

// automator 的门超时分支(Task B2):到期 → CAS 置 timeout + 按建议代选落范围
// (A3 服务层等价物:双表、整组同一把 scope_revision)+ roomnotice 通知。
func TestAutomatorGateExpiredSelectsBySuggestion(t *testing.T) {
	pool := testdb.Open(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	const issueID = "iss_gate_timeout_flow"
	fixture := testdb.SeedProject(t, pool, "", "", "acme/checkout", "acme/shared-lib")
	testdb.SeedIssue(t, pool, fixture, issueID, "acme/checkout")
	if _, err := pool.Exec(ctx, `UPDATE repomesh_issues.issues SET hitl_mode='ai' WHERE id=$1`, issueID); err != nil {
		t.Fatalf("fixture: %v", err)
	}
	// ①② 已完成(分析足够 + 候选在库),只剩门:超时代选后下一拍就是 ③ 分档。
	if _, err := pool.Exec(ctx, `INSERT INTO repomesh_issues.issue_discoveries
		(issue_id, project_id, requirement_text, analysis, candidates, idempotency_ledger)
		VALUES ($1, $2, '改结算与共享库', '{"sufficient":true,"analyzed_requirement":"改结算与共享库"}'::jsonb,
		'{"items":[{"repository_name":"acme/checkout","score":0.9,"agent_tier":"required","rationale":"点名"},
		          {"repository_name":"acme/shared-lib","score":0.8,"agent_tier":"required","rationale":"依赖"}],"llm_used":true}'::jsonb,
		'{}'::jsonb)`, issueID, fixture.ID); err != nil {
		t.Fatalf("fixture: %v", err)
	}
	past := time.Now().UTC().Add(-time.Minute)
	if err := discovery.New(pool).OpenGate(ctx, issueID, []string{"acme/checkout", "acme/shared-lib"}, &past); err != nil {
		t.Fatalf("open gate: %v", err)
	}
	automator := newDiscoveryAutomator(discovery.New(pool), pool, nil)
	if !automator.step(ctx) {
		t.Fatal("门超时分支应完成一次工作(代选落范围)")
	}
	var gateState, decidedBy string
	if err := pool.QueryRow(ctx, `SELECT scope_gate->>'state', scope_gate->>'decided_by'
		FROM repomesh_issues.issue_discoveries WHERE issue_id=$1`, issueID).Scan(&gateState, &decidedBy); err != nil {
		t.Fatalf("读门: %v", err)
	}
	if gateState != "resolved" || decidedBy != "timeout" {
		t.Fatalf("超时应 CAS 置 resolved(timeout): %q %q", gateState, decidedBy)
	}
	revisions := map[string]string{}
	rows, err := pool.Query(ctx, `SELECT repository_id, scope_revision FROM repomesh_issues.issue_repository_scope WHERE issue_id=$1`, issueID)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var repositoryID, scopeRevision string
		if err = rows.Scan(&repositoryID, &scopeRevision); err != nil {
			t.Fatal(err)
		}
		revisions[repositoryID] = scopeRevision
	}
	rows.Close()
	if len(revisions) != 2 {
		t.Fatalf("建议集合应整体落范围(2 仓): %+v", revisions)
	}
	shared := ""
	for _, scopeRevision := range revisions {
		if shared == "" {
			shared = scopeRevision
		} else if scopeRevision != shared {
			t.Fatalf("整组应同一把 scope_revision: %+v", revisions)
		}
	}
	var contentCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM repomesh_issues.issue_content_scope WHERE issue_id=$1`, issueID).Scan(&contentCount); err != nil || contentCount != 2 {
		t.Fatalf("内容范围应双表同组: %d %v", contentCount, err)
	}
}
