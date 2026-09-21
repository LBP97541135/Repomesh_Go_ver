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
	if got := autohostStep(progress); got != 8 {
		t.Fatalf("已查漏后应进 ③ 分档(步 8),得到 %d", got)
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

// 「点了才生成」全链路(spec 2026-09-20 修订):门先出现(建议为空)→ 点「让 AI 定」
// (ai_requested)→ 协调器只登记候选生成意图(不空转、不进 3 次熔断)→ 建议落地 →
// 自动采纳为范围(resolved/ai)+ 唤醒入选仓库团队。
//
// 门在候选块**已经落库**的情况下仍 pending,且仍被协调器捡起 —— 盖住
// 「门不因候选落库而消失」这条(SQL 级)。
func TestAutomatorAIRequestedGeneratesThenAdopts(t *testing.T) {
	pool := testdb.Open(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	const issueID = "iss_gate_ai_requested_flow"
	fixture := testdb.SeedProject(t, pool, "", "", "acme/checkout", "acme/shared-lib")
	testdb.SeedIssue(t, pool, fixture, issueID, "acme/checkout")
	if _, err := pool.Exec(ctx, `UPDATE repomesh_issues.issues SET hitl_mode='ai' WHERE id=$1`, issueID); err != nil {
		t.Fatalf("fixture: %v", err)
	}
	// ① 分析已足够、② 候选块已在库 —— 门却仍 pending(建议为空)。
	if _, err := pool.Exec(ctx, `INSERT INTO repomesh_issues.issue_discoveries
		(issue_id, project_id, requirement_text, analysis, candidates, idempotency_ledger)
		VALUES ($1, $2, '改结算与共享库', '{"sufficient":true,"analyzed_requirement":"改结算与共享库"}'::jsonb,
		'{"items":[{"repository_name":"acme/checkout","score":0.9,"agent_tier":"required","rationale":"点名"},
		          {"repository_name":"acme/shared-lib","score":0.8,"agent_tier":"required","rationale":"依赖"}],"llm_used":true}'::jsonb,
		'{}'::jsonb)`, issueID, fixture.ID); err != nil {
		t.Fatalf("fixture: %v", err)
	}
	service := discovery.New(pool)
	future := time.Now().UTC().Add(10 * time.Minute)
	if err := service.OpenGate(ctx, issueID, nil, &future); err != nil {
		t.Fatalf("open gate: %v", err)
	}
	if flipped, err := service.MarkGateAIRequested(ctx, issueID); err != nil || !flipped {
		t.Fatalf("置 ai_requested: %v %v", flipped, err)
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
		t.Fatal("ai_requested 的门(带未来截止、候选块已在库)应入选自动托管")
	}
	if !progress.gateAIRequested || progress.gateSuggestedCount != 0 {
		t.Fatalf("投影应 gateAIRequested 且建议为空: %+v", progress)
	}
	if !progress.hasCandidates {
		t.Fatalf("候选块应在库(门仍不许因此消失): %+v", progress)
	}
	if got := autohostStep(progress); got != 4 {
		t.Fatalf("应停在「登记候选意图」步(4),得到 %d", got)
	}

	// 等建议期间反复 tick:每拍都返回 true(在做正确的事),但**不进熔断计数**。
	for i := 0; i < 5; i++ {
		delete(automator.backoff, issueID)
		if !automator.step(ctx) {
			t.Fatalf("第 %d 拍应登记候选意图(返回 true)", i)
		}
	}
	if len(automator.attempts) != 0 {
		t.Fatalf("等建议期间不得累计熔断计数: %v", automator.attempts)
	}
	var runCount, runStep int
	if err := pool.QueryRow(ctx, `SELECT count(*), COALESCE(max(step),0) FROM repomesh_issues.planning_runs
		WHERE issue_id=$1 AND state='pending'`, issueID).Scan(&runCount, &runStep); err != nil {
		t.Fatalf("读 planning_runs: %v", err)
	}
	if runCount != 1 || runStep != discovery.PlanningCandidates {
		t.Fatalf("反复 tick 只应登记一次候选意图(幂等): count=%d step=%d", runCount, runStep)
	}

	// 建议落地(② 候选产物应用)→ 下一拍自动采纳为范围。
	if err := service.FillGateSuggested(ctx, issueID, []string{"acme/checkout", "acme/shared-lib"}); err != nil {
		t.Fatalf("fill suggestion: %v", err)
	}
	var wokeProject string
	var wokeRepos []string
	adopter := newDiscoveryAutomator(service, pool, nil).withWake(func(_ context.Context, projectID string, repositoryIDs []string) {
		wokeProject, wokeRepos = projectID, repositoryIDs
	})
	if !adopter.step(ctx) {
		t.Fatal("建议就绪后应完成一次工作(采纳为范围)")
	}
	var gateState, decidedBy string
	if err := pool.QueryRow(ctx, `SELECT scope_gate->>'state', scope_gate->>'decided_by'
		FROM repomesh_issues.issue_discoveries WHERE issue_id=$1`, issueID).Scan(&gateState, &decidedBy); err != nil {
		t.Fatalf("读门: %v", err)
	}
	if gateState != "resolved" || decidedBy != "ai" {
		t.Fatalf("应 CAS 置 resolved(ai): %q %q", gateState, decidedBy)
	}
	var scopeCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM repomesh_issues.issue_repository_scope WHERE issue_id=$1`, issueID).Scan(&scopeCount); err != nil || scopeCount != 2 {
		t.Fatalf("建议应整体落范围(2 仓): %d %v", scopeCount, err)
	}
	if wokeProject != fixture.ID || len(wokeRepos) != 2 {
		t.Fatalf("采纳后应唤醒入选仓库的团队: project=%q repos=%v", wokeProject, wokeRepos)
	}
}
