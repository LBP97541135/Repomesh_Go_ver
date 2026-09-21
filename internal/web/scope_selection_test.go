package web

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"repomesh.local/repomesh/internal/access"
	"repomesh.local/repomesh/internal/discovery"
	"repomesh.local/repomesh/internal/issues"
	"repomesh.local/repomesh/internal/modelbudget"
	"repomesh.local/repomesh/internal/projects"
	"repomesh.local/repomesh/internal/testdb"
)

// scopeSelectionHarness 是"项目 + 两个仓库 + issue + 确认端点"的一次性样板:
// 确认端点的前置条件很多(项目上下文版本、仓在项目内、issue 已建项),几个测试
// 共用一份,免得各抄一遍。
type scopeSelectionHarness struct {
	server    *browserTestServer
	principal access.ProjectPrincipal
	projectID string
	revision  string
	issueID   string
	post      func(body string) (int, map[string]any)
}

func newScopeSelectionHarness(t *testing.T, issueID string) scopeSelectionHarness {
	t.Helper()
	assets := t.TempDir()
	if err := os.WriteFile(filepath.Join(assets, "index.html"), []byte("<!doctype html>"), 0600); err != nil {
		t.Fatal(err)
	}
	server := startProjectBrowserServer(t, assets)
	ctx := context.Background()
	response := httptest.NewRecorder()
	server.login(response, httptest.NewRequest("GET", "https://fixture/__test/login?actor=a", nil))
	var cookieValue string
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == sessionCookie {
			cookieValue = cookie.Value
		}
	}
	if cookieValue == "" {
		t.Fatal("fixture login failed")
	}
	session, err := server.auth.Session(ctx, cookieValue)
	if err != nil {
		t.Fatal(err)
	}
	principal, err := server.auth.AuthenticateProjectRequest(ctx, cookieValue, session.CSRFToken, true)
	if err != nil {
		t.Fatal(err)
	}
	projectService := projects.New(server.pool, server.auth)
	issueService := issues.New(server.pool, server.auth, projectService, modelbudget.New())
	raw, err := projects.ParseRawInput([]byte(`{"name":"Scope gate","purpose":"gate","repositoryIds":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	command, err := projects.PrepareCreate(raw, "60000000-0000-4000-8000-000000000021")
	if err != nil {
		t.Fatal(err)
	}
	created, err := projectService.Create(ctx, principal, command)
	if err != nil {
		t.Fatal(err)
	}
	pid := created.Receipt.ProjectID
	// 等 fake provider 把两个候选仓写进仓库目录,才能挂到项目上。
	deadline := time.Now().Add(5 * time.Second)
	for {
		page, lookupErr := server.auth.Repositories(ctx, cookieValue, access.RepositoryQuery{Limit: 50})
		if lookupErr == nil && len(page.Items) >= 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("candidate fixture unavailable: %v", lookupErr)
		}
		time.Sleep(10 * time.Millisecond)
	}
	raw, err = projects.ParseRawInput([]byte(`{"expectedProjectRevision":"` + created.Receipt.ProjectRevision + `","repositoryIdsToAdd":["` + server.fixtures.RepositoryA + `","` + server.fixtures.RepositoryB + `"],"configuration":{"modelProfile":{"mode":"inherit"},"executionProfile":{"mode":"inherit"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	update, err := projects.PrepareUpdate(raw, pid, "60000000-0000-4000-8000-000000000022")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = projectService.Update(ctx, principal, update); err != nil {
		t.Fatal(err)
	}
	var revision string
	if err = server.pool.QueryRow(ctx, `SELECT creation_context_revision FROM repomesh_projects.projects WHERE id=$1`, pid).Scan(&revision); err != nil {
		t.Fatal(err)
	}
	// issue 的 initial_configuration_revision 有外键(必须指向本项目真实的
	// configuration_revisions 行);端点乐观锁用的 creation_context_revision
	// 与它不是同一个值域,分开取。
	var configRevision string
	if err = server.pool.QueryRow(ctx, `SELECT revision FROM repomesh_projects.configuration_revisions WHERE project_id=$1 ORDER BY created_at DESC LIMIT 1`, pid).Scan(&configRevision); err != nil {
		t.Fatal(err)
	}
	// issue 走完整建项聚合(fixture 触发器要求全形状),带仓 A。
	fixture := testdb.ProjectFixture{ID: pid, OwnerID: principal.ActorID(), ConfigurationID: configRevision,
		Repositories: map[string]string{"a": server.fixtures.RepositoryA, "b": server.fixtures.RepositoryB}}
	testdb.SeedIssue(t, server.pool, fixture, issueID, "a")
	mux := http.NewServeMux()
	registerIssueRoutes(mux, Auth{Service: server.auth, Origin: "https://fixture"}, Issues{Service: issueService})
	post := func(body string) (int, map[string]any) {
		request := httptest.NewRequest(http.MethodPost, "https://fixture/api/projects/"+pid+"/issues/"+issueID+"/scope/selection", strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Origin", "https://fixture")
		request.Header.Set("X-CSRF-Token", session.CSRFToken)
		request.AddCookie(&http.Cookie{Name: sessionCookie, Value: cookieValue})
		recorder := httptest.NewRecorder()
		mux.ServeHTTP(recorder, request)
		data, _ := io.ReadAll(recorder.Result().Body)
		out := map[string]any{}
		_ = json.Unmarshal(data, &out)
		return recorder.Code, out
	}
	return scopeSelectionHarness{server: server, principal: principal, projectID: pid, revision: revision, issueID: issueID, post: post}
}

// 选仓门批量确认端点(spec 2026-09-20 §3.2):一个事务里同时写
// issue_repository_scope 和 issue_content_scope(双表、整组同一把
// scope_revision),并把门 CAS 置 resolved。空数组 422;revision 不匹配或仓
// 不在项目 409;幂等键重放 200 且不重写。
func TestIssueScopeSelectionConfirm(t *testing.T) {
	h := newScopeSelectionHarness(t, "iss_scope_selection_confirm")
	ctx := context.Background()
	if err := discovery.New(h.server.pool).OpenGate(ctx, h.issueID, []string{"fixture-a/one"}, nil); err != nil {
		t.Fatal(err)
	}
	reposA := `["` + h.server.fixtures.RepositoryA + `"]`
	reposAB := `["` + h.server.fixtures.RepositoryA + `","` + h.server.fixtures.RepositoryB + `"]`

	// 空数组(manual)→ 422。
	if code, _ := h.post(`{"repositoryIds":[],"decidedBy":"manual","idempotencyKey":"k-empty","expectedCreationContextRevision":"` + h.revision + `"}`); code != http.StatusUnprocessableEntity {
		t.Fatalf("空仓库数组应 422,得到 %d", code)
	}
	// 线缆键名必须是 camelCase(Task B4):snake_case 的键不被解码器认,仓库数组
	// 解成空 → 422。这条锁住 /scope/selection 的对外字段名,防止有人改回下划线。
	if code, _ := h.post(`{"repository_ids":` + reposA + `,"decided_by":"manual","idempotency_key":"k-snake","expected_creation_context_revision":"` + h.revision + `"}`); code != http.StatusUnprocessableEntity {
		t.Fatalf("snake_case 线缆键不应被接受(约定 camelCase),得到 %d", code)
	}
	// revision 不匹配 → 409。
	if code, _ := h.post(`{"repositoryIds":` + reposA + `,"decidedBy":"manual","idempotencyKey":"k-stale","expectedCreationContextRevision":"stale"}`); code != http.StatusConflict {
		t.Fatalf("revision 不匹配应 409,得到 %d", code)
	}
	// 仓不在项目 → 409。
	if code, _ := h.post(`{"repositoryIds":["repo_00000000000000000999"],"decidedBy":"manual","idempotencyKey":"k-outside","expectedCreationContextRevision":"` + h.revision + `"}`); code != http.StatusConflict {
		t.Fatalf("仓不在项目应 409,得到 %d", code)
	}
	// 成功:200 committed;两表同组、同一把 scope_revision;门 resolved。
	code, out := h.post(`{"repositoryIds":` + reposAB + `,"decidedBy":"manual","idempotencyKey":"confirm-1","expectedCreationContextRevision":"` + h.revision + `"}`)
	if code != http.StatusOK || out["status"] != "committed" || out["repositoryCount"] != float64(2) {
		t.Fatalf("确认应 200 committed(2): %d %+v", code, out)
	}
	revisions := map[string]string{}
	rows, err := h.server.pool.Query(ctx, `SELECT repository_id, scope_revision FROM repomesh_issues.issue_repository_scope WHERE issue_id=$1`, h.issueID)
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
		t.Fatalf("仓库范围应两行: %+v", revisions)
	}
	shared := ""
	for _, scopeRevision := range revisions {
		if scopeRevision == "scope-1" {
			t.Fatalf("建项种子 revision 不该保留: %+v", revisions)
		}
		if shared == "" {
			shared = scopeRevision
		} else if scopeRevision != shared {
			t.Fatalf("整组应同一把 scope_revision: %+v", revisions)
		}
	}
	var contentCount int
	if err = h.server.pool.QueryRow(ctx, `SELECT count(*) FROM repomesh_issues.issue_content_scope WHERE issue_id=$1`, h.issueID).Scan(&contentCount); err != nil || contentCount != 2 {
		t.Fatalf("内容范围应两行: %d %v", contentCount, err)
	}
	var gateState, decidedBy string
	if err = h.server.pool.QueryRow(ctx, `SELECT scope_gate->>'state', scope_gate->>'decided_by' FROM repomesh_issues.issue_discoveries WHERE issue_id=$1`, h.issueID).Scan(&gateState, &decidedBy); err != nil || gateState != "resolved" || decidedBy != "manual" {
		t.Fatalf("门应 resolved(manual): %q %q %v", gateState, decidedBy, err)
	}
	// 幂等键重放:200,状态 replayed,范围 revision 原样不动。
	code, out = h.post(`{"repositoryIds":` + reposAB + `,"decidedBy":"manual","idempotencyKey":"confirm-1","expectedCreationContextRevision":"` + h.revision + `"}`)
	if code != http.StatusOK || out["status"] != "replayed" || out["repositoryCount"] != float64(2) {
		t.Fatalf("重放应 200 replayed(2): %d %+v", code, out)
	}
	if code, _ = h.post(`{"repositoryIds":` + reposAB + `,"decidedBy":"timeout","idempotencyKey":"confirm-1","expectedCreationContextRevision":"` + h.revision + `"}`); code != http.StatusOK {
		t.Fatalf("同键重放不因 decided_by 不同而改写: %d", code)
	}
	var after string
	if err = h.server.pool.QueryRow(ctx, `SELECT scope_revision FROM repomesh_issues.issue_repository_scope WHERE issue_id=$1 AND repository_id=$2`, h.issueID, h.server.fixtures.RepositoryA).Scan(&after); err != nil || after != shared {
		t.Fatalf("重放不得重写范围 revision: %q 期望 %q %v", after, shared, err)
	}
	if err = h.server.pool.QueryRow(ctx, `SELECT scope_gate->>'decided_by' FROM repomesh_issues.issue_discoveries WHERE issue_id=$1`, h.issueID).Scan(&decidedBy); err != nil || decidedBy != "manual" {
		t.Fatalf("重放不得改写决定人: %q %v", decidedBy, err)
	}
}

// 「让 AI 定」的两段(spec 2026-09-20 修订):门先出现、建议为空时点它,只记下
// ai_requested 并返回同状态(status=ai_requested,不写范围、不关门);建议落地后
// 再点(或协调器自动)才采纳建议为范围、关门。
func TestIssueScopeSelectionAIRequestedThenAdopt(t *testing.T) {
	h := newScopeSelectionHarness(t, "iss_scope_selection_ai")
	ctx := context.Background()
	// 门先出现,建议为空(新顺序:① 分析后开门)。
	if err := discovery.New(h.server.pool).OpenGate(ctx, h.issueID, nil, nil); err != nil {
		t.Fatal(err)
	}
	blank := `{"repositoryIds":[],"decidedBy":"ai","idempotencyKey":"ai-blank","expectedCreationContextRevision":"` + h.revision + `"}`
	// 建项聚合本就种下一行范围(仓 A);ai_requested 不得让它变多。
	var scopeBefore int
	if err := h.server.pool.QueryRow(ctx, `SELECT count(*) FROM repomesh_issues.issue_repository_scope WHERE issue_id=$1`, h.issueID).Scan(&scopeBefore); err != nil {
		t.Fatal(err)
	}
	code, out := h.post(blank)
	if code != http.StatusOK || out["status"] != "ai_requested" || out["repositoryCount"] != float64(0) {
		t.Fatalf("建议为空时应 200 ai_requested(0): %d %+v", code, out)
	}
	// 门仍 pending 且 ai_requested=true;范围未写。
	var gateState string
	var aiRequested bool
	if err := h.server.pool.QueryRow(ctx, `SELECT scope_gate->>'state', COALESCE((scope_gate->>'ai_requested')::bool, false)
		FROM repomesh_issues.issue_discoveries WHERE issue_id=$1`, h.issueID).Scan(&gateState, &aiRequested); err != nil || gateState != "pending" || !aiRequested {
		t.Fatalf("空请求后门应 pending 且 ai_requested=true: %q %v %v", gateState, aiRequested, err)
	}
	var scopeRows int
	if err := h.server.pool.QueryRow(ctx, `SELECT count(*) FROM repomesh_issues.issue_repository_scope WHERE issue_id=$1`, h.issueID).Scan(&scopeRows); err != nil {
		t.Fatal(err)
	}
	if scopeRows != scopeBefore {
		t.Fatalf("空请求不得写范围(建项种子 %d 行应原样): 得到 %d 行", scopeBefore, scopeRows)
	}
	// 同幂等键重放:仍返回同状态 ai_requested(不写范围、不关门)。
	if code, out = h.post(blank); code != http.StatusOK || out["status"] != "ai_requested" {
		t.Fatalf("同键重放应 200 ai_requested: %d %+v", code, out)
	}
	// 建议落地(等价 ② 候选产物应用)→ 再点「让 AI 定」(新键)→ 直接采纳为范围。
	if err := discovery.New(h.server.pool).FillGateSuggested(ctx, h.issueID, []string{"fixture-a/one"}); err != nil {
		t.Fatal(err)
	}
	code, out = h.post(`{"repositoryIds":[],"decidedBy":"ai","idempotencyKey":"ai-adopt","expectedCreationContextRevision":"` + h.revision + `"}`)
	if code != http.StatusOK || out["status"] != "committed" || out["repositoryCount"] != float64(1) {
		t.Fatalf("有建议时应 200 committed(1): %d %+v", code, out)
	}
	var decidedBy string
	if err := h.server.pool.QueryRow(ctx, `SELECT scope_gate->>'state', scope_gate->>'decided_by'
		FROM repomesh_issues.issue_discoveries WHERE issue_id=$1`, h.issueID).Scan(&gateState, &decidedBy); err != nil || gateState != "resolved" || decidedBy != "ai" {
		t.Fatalf("采纳后门应 resolved(ai): %q %q %v", gateState, decidedBy, err)
	}
	var scoped string
	if err := h.server.pool.QueryRow(ctx, `SELECT repository_id FROM repomesh_issues.issue_repository_scope WHERE issue_id=$1`, h.issueID).Scan(&scoped); err != nil {
		t.Fatalf("范围应落库: %v", err)
	}
	if scoped != h.server.fixtures.RepositoryA {
		t.Fatalf("范围应采纳建议对应的仓: %s 期望 %s", scoped, h.server.fixtures.RepositoryA)
	}
	var contentRows int
	if err := h.server.pool.QueryRow(ctx, `SELECT count(*) FROM repomesh_issues.issue_content_scope WHERE issue_id=$1`, h.issueID).Scan(&contentRows); err != nil || contentRows != 1 {
		t.Fatalf("内容范围应同组: %d %v", contentRows, err)
	}
}

// 确认进房的文案：三种判定各说各的，别把"人勾的"说成"AI 定的"。
func TestScopeConfirmedNotice(t *testing.T) {
	cases := map[string]string{"manual": "人勾选", "ai": "AI 定", "timeout": "超时代选"}
	for decidedBy, want := range cases {
		got := scopeConfirmedNotice(decidedBy, 3)
		if !strings.Contains(got, want) || !strings.Contains(got, "3") {
			t.Fatalf("%s: %q", decidedBy, got)
		}
	}
}
