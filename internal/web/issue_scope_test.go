package web

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"repomesh.local/repomesh/internal/access"
	"repomesh.local/repomesh/internal/github"
	"repomesh.local/repomesh/internal/issues"
	"repomesh.local/repomesh/internal/modelbudget"
	"repomesh.local/repomesh/internal/models"
	"repomesh.local/repomesh/internal/projects"
)

type appDeniedProvider struct{ *browserProvider }

func (p appDeniedProvider) AppCapability(context.Context, string, string) (github.Capability, error) {
	now := time.Now().UTC()
	return github.Capability{Status: "denied", ReasonCodes: []string{"APP_INSTALLATION_MISSING"}, ObservedAt: &now}, nil
}

func TestProjectDraftIssueReadinessAndScope(t *testing.T) {
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
	raw, err := projects.ParseRawInput([]byte(`{"name":"Scope draft","purpose":"Create then join","repositoryIds":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	command, err := projects.PrepareCreate(raw, "50000000-0000-4000-8000-000000000011")
	if err != nil {
		t.Fatal(err)
	}
	created, err := projectService.Create(ctx, principal, command)
	if err != nil {
		t.Fatal(err)
	}
	pid := created.Receipt.ProjectID
	options, err := issueService.Options(ctx, principal, pid, issues.ParsePageQuery("", 1))
	// 建项不再选仓(2026-09-20):available=0 不再产生 NO_AVAILABLE_REPOSITORIES,
	// 也不再把 CanSubmit 压成 false——能不能提交只看配置与 App 就绪。
	if err != nil || options.CanSubmit || slices.Contains(options.BlockingReasons, "NO_AVAILABLE_REPOSITORIES") {
		t.Fatalf("empty project options=%+v err=%v", options, err)
	}
	if !slices.Contains(options.BlockingReasons, "CONFIGURATION_NOT_READY") {
		t.Fatalf("missing model binding advertised as ready: %+v", options)
	}

	// Save a complete model through the product service: the older browser
	// fixture catalog intentionally has no provider snapshot binding.
	modelService := models.New(server.pool, server.auth, server.store, projects.NewCatalogWriter())
	modelRaw, err := models.ReadSaveBody([]byte(`{"providerId":null,"expectedRevision":null,"name":"Scope fixture","baseUrl":"https://fixture.invalid/v1","apiFormat":"openai_chat_completions","secret":{"mode":"replace","value":"scope-fixture-key"},"models":[{"id":null,"modelId":"fixture-model","displayName":"Fixture","contextWindow":16000,"maxOutputTokens":1000,"reasoning":false,"vision":false}]}`))
	if err != nil {
		t.Fatal(err)
	}
	modelCommand, err := models.NewSaveCommand("50000000-0000-4000-8000-000000000010", "scope-model", modelRaw)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = modelService.Save(ctx, principal, modelCommand); err != nil {
		t.Fatal(err)
	}
	if _, err = server.pool.Exec(ctx, `UPDATE repomesh_projects.defaults SET profile_id=(SELECT profile_id FROM repomesh_models.profile_links WHERE owner=$1 LIMIT 1) WHERE actor=$1 AND kind='model'`, principal.ActorID()); err != nil {
		t.Fatal(err)
	}

	// Discovery runs only against the fake provider; wait for the candidate ledger.
	deadline := time.Now().Add(5 * time.Second)
	for {
		page, e := server.auth.Repositories(ctx, cookieValue, access.RepositoryQuery{Limit: 50})
		if e == nil && len(page.Items) >= 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("candidate fixture unavailable: %v", e)
		}
		time.Sleep(10 * time.Millisecond)
	}
	raw, err = projects.ParseRawInput([]byte(`{"expectedProjectRevision":"` + created.Receipt.ProjectRevision + `","repositoryIdsToAdd":["` + server.fixtures.RepositoryA + `"],"configuration":{"modelProfile":{"mode":"inherit"},"executionProfile":{"mode":"inherit"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	update, err := projects.PrepareUpdate(raw, pid, "50000000-0000-4000-8000-000000000012")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = projectService.Update(ctx, principal, update); err != nil {
		t.Fatal(err)
	}
	options, err = issueService.Options(ctx, principal, pid, issues.ParsePageQuery("", 1))
	if err != nil || options.CanSubmit || slices.Contains(options.BlockingReasons, "CONFIGURATION_NOT_READY") || slices.Contains(options.BlockingReasons, "NO_AVAILABLE_REPOSITORIES") || !slices.Contains(options.BlockingReasons, "APP_AUTHORIZATION_UNCONFIRMED") || len(options.Repositories) != 1 || options.Repositories[0].RepositoryID != server.fixtures.RepositoryA {
		t.Fatalf("joined options=%+v err=%v", options, err)
	}
	deniedAuth := access.New(server.pool, server.store, appDeniedProvider{server.provider})
	deniedService := issues.New(server.pool, deniedAuth, projectService, modelbudget.New())
	denied, err := deniedService.Options(ctx, principal, pid, issues.ParsePageQuery("", 1))
	// 全部仓库 App 拒绝 = available 0:按旧逻辑会压 NO_AVAILABLE_REPOSITORIES,
	// 现在仓库多少不再决定可提交性,这个 reason 必须消失。
	if err != nil || denied.CanSubmit || slices.Contains(denied.BlockingReasons, "NO_AVAILABLE_REPOSITORIES") || len(denied.Repositories) != 1 || denied.Repositories[0].Selectable {
		t.Fatalf("App denied options=%+v err=%v", denied, err)
	}
	create := func(repo, key string) (issues.CreationResult, error) {
		body, _ := json.Marshal(map[string]any{"expectedCreationContextRevision": options.CreationContextRevision, "title": "Scope issue", "description": "Scope issue", "repositoryIds": []string{repo}, "conversation": map[string]string{"mode": "new"}})
		cmd, e := issues.ParsePageCommand(pid, key, "scope-test", body)
		if e != nil {
			return issues.CreationResult{}, e
		}
		return issueService.CreatePage(ctx, principal, cmd)
	}
	if _, err = create(server.fixtures.RepositoryB, "50000000-0000-4000-8000-000000000013"); err == nil {
		t.Fatal("out-of-project Issue creation accepted")
	}
	var count int
	if err = server.pool.QueryRow(ctx, `SELECT count(*) FROM repomesh_issues.creation_operations WHERE project_id=$1`, pid).Scan(&count); err != nil || count != 0 {
		t.Fatalf("rejection left partial aggregate: %d %v", count, err)
	}
	// This fixture has no runtime App key. Even an allowed repository must
	// remain unsubmitted, exactly as the options projection now reports.
	_, err = create(server.fixtures.RepositoryA, "50000000-0000-4000-8000-000000000014")
	var unavailable *access.Failure
	if !errors.As(err, &unavailable) || unavailable.Code != "AUTHORIZATION_UNCONFIRMED" {
		t.Fatalf("missing runtime credential: %v", err)
	}

	// A page with no rows after the cursor still reports global readiness.
	tail, err := issueService.Options(ctx, principal, pid, issues.ParsePageQuery(server.fixtures.RepositoryA, 1))
	if err != nil || tail.CanSubmit != options.CanSubmit || len(tail.Repositories) != 0 {
		t.Fatalf("readiness depends on page: %+v %v", tail, err)
	}
}
