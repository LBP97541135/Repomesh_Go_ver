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

// 选仓门批量确认端点(spec 2026-09-20 §3.2):一个事务里同时写
// issue_repository_scope 和 issue_content_scope(双表、整组同一把
// scope_revision),并把门 CAS 置 resolved。空数组 422;revision 不匹配或仓
// 不在项目 409;幂等键重放 200 且不重写。
func TestIssueScopeSelectionConfirm(t *testing.T) {
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
	// issue 走完整建项聚合(fixture 触发器要求全形状),带仓 A;
	// 确认 {A,B} 正好同时覆盖"已有行刷新 revision"与"新行插入"两条路径。
	const issueID = "iss_scope_selection_confirm"
	fixture := testdb.ProjectFixture{ID: pid, OwnerID: principal.ActorID(), ConfigurationID: configRevision,
		Repositories: map[string]string{"a": server.fixtures.RepositoryA, "b": server.fixtures.RepositoryB}}
	testdb.SeedIssue(t, server.pool, fixture, issueID, "a")
	if err = discovery.New(server.pool).OpenGate(ctx, issueID, []string{"fixture-a/one"}, nil); err != nil {
		t.Fatal(err)
	}
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
	reposA := `["` + server.fixtures.RepositoryA + `"]`
	reposAB := `["` + server.fixtures.RepositoryA + `","` + server.fixtures.RepositoryB + `"]`

	// 空数组 → 422。
	if code, _ := post(`{"repositoryIds":[],"decidedBy":"manual","idempotencyKey":"k-empty","expectedCreationContextRevision":"` + revision + `"}`); code != http.StatusUnprocessableEntity {
		t.Fatalf("空仓库数组应 422,得到 %d", code)
	}
	// revision 不匹配 → 409。
	if code, _ := post(`{"repositoryIds":` + reposA + `,"decidedBy":"manual","idempotencyKey":"k-stale","expectedCreationContextRevision":"stale"}`); code != http.StatusConflict {
		t.Fatalf("revision 不匹配应 409,得到 %d", code)
	}
	// 仓不在项目 → 409。
	if code, _ := post(`{"repositoryIds":["repo_00000000000000000999"],"decidedBy":"manual","idempotencyKey":"k-outside","expectedCreationContextRevision":"` + revision + `"}`); code != http.StatusConflict {
		t.Fatalf("仓不在项目应 409,得到 %d", code)
	}
	// 成功:200 committed;两表同组、同一把 scope_revision;门 resolved。
	code, out := post(`{"repositoryIds":` + reposAB + `,"decidedBy":"manual","idempotencyKey":"confirm-1","expectedCreationContextRevision":"` + revision + `"}`)
	if code != http.StatusOK || out["status"] != "committed" || out["repositoryCount"] != float64(2) {
		t.Fatalf("确认应 200 committed(2): %d %+v", code, out)
	}
	revisions := map[string]string{}
	rows, err := server.pool.Query(ctx, `SELECT repository_id, scope_revision FROM repomesh_issues.issue_repository_scope WHERE issue_id=$1`, issueID)
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
	if err = server.pool.QueryRow(ctx, `SELECT count(*) FROM repomesh_issues.issue_content_scope WHERE issue_id=$1`, issueID).Scan(&contentCount); err != nil || contentCount != 2 {
		t.Fatalf("内容范围应两行: %d %v", contentCount, err)
	}
	var gateState, decidedBy string
	if err = server.pool.QueryRow(ctx, `SELECT scope_gate->>'state', scope_gate->>'decided_by' FROM repomesh_issues.issue_discoveries WHERE issue_id=$1`, issueID).Scan(&gateState, &decidedBy); err != nil || gateState != "resolved" || decidedBy != "manual" {
		t.Fatalf("门应 resolved(manual): %q %q %v", gateState, decidedBy, err)
	}
	// 幂等键重放:200,状态 replayed,范围 revision 原样不动。
	code, out = post(`{"repositoryIds":` + reposAB + `,"decidedBy":"manual","idempotencyKey":"confirm-1","expectedCreationContextRevision":"` + revision + `"}`)
	if code != http.StatusOK || out["status"] != "replayed" || out["repositoryCount"] != float64(2) {
		t.Fatalf("重放应 200 replayed(2): %d %+v", code, out)
	}
	if code, _ = post(`{"repositoryIds":` + reposAB + `,"decidedBy":"timeout","idempotencyKey":"confirm-1","expectedCreationContextRevision":"` + revision + `"}`); code != http.StatusOK {
		t.Fatalf("同键重放不因 decided_by 不同而改写: %d", code)
	}
	var after string
	if err = server.pool.QueryRow(ctx, `SELECT scope_revision FROM repomesh_issues.issue_repository_scope WHERE issue_id=$1 AND repository_id=$2`, issueID, server.fixtures.RepositoryA).Scan(&after); err != nil || after != shared {
		t.Fatalf("重放不得重写范围 revision: %q 期望 %q %v", after, shared, err)
	}
	if err = server.pool.QueryRow(ctx, `SELECT scope_gate->>'decided_by' FROM repomesh_issues.issue_discoveries WHERE issue_id=$1`, issueID).Scan(&decidedBy); err != nil || decidedBy != "manual" {
		t.Fatalf("重放不得改写决定人: %q %v", decidedBy, err)
	}
}
