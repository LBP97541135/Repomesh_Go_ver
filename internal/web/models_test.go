package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"repomesh.local/repomesh/internal/access"
	"repomesh.local/repomesh/internal/models"
	"repomesh.local/repomesh/internal/projects"
)

type modelErrorBody struct {
	Error struct {
		Code        string `json:"code"`
		FieldErrors []struct {
			Field string `json:"field"`
			Code  string `json:"code"`
		} `json:"fieldErrors"`
	} `json:"error"`
}

func TestPostgresModelHTTPContract(t *testing.T) {
	assets := t.TempDir()
	if err := os.WriteFile(filepath.Join(assets, "index.html"), []byte("<!doctype html><title>B04</title>"), 0600); err != nil {
		t.Fatal(err)
	}
	server := startProjectBrowserServer(t, assets)
	ctx := context.Background()
	var seen sync.Mutex
	var bodies []string
	newClient := func() *http.Client {
		jar, _ := cookiejar.New(nil)
		client := server.server.Client()
		client.Jar = jar
		client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		return client
	}
	send := func(client *http.Client, method, path, body, contentType, origin, csrf, key string) (int, []byte, http.Header) {
		t.Helper()
		req, err := http.NewRequest(method, server.server.URL+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		if csrf != "" {
			req.Header.Set("X-CSRF-Token", csrf)
		}
		if key != "" {
			req.Header.Set("Idempotency-Key", key)
		}
		response, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		data, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}
		seen.Lock()
		bodies = append(bodies, string(data))
		seen.Unlock()
		return response.StatusCode, data, response.Header
	}
	client := newClient()
	request := func(method, path, body, origin, csrf, key string) (int, []byte, http.Header) {
		t.Helper()
		contentType := ""
		if body != "" {
			contentType = "application/json"
		}
		return send(client, method, path, body, contentType, origin, csrf, key)
	}
	wantError := func(status int, data []byte, wantStatus int, code string) modelErrorBody {
		t.Helper()
		var body modelErrorBody
		if status != wantStatus || json.Unmarshal(data, &body) != nil || body.Error.Code != code {
			t.Fatalf("status=%d body=%s; want %d %s", status, data, wantStatus, code)
		}
		return body
	}
	count := func(query string, args ...any) int {
		t.Helper()
		var n int
		if err := server.pool.QueryRow(ctx, query, args...).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	modelSecrets := `SELECT count(*) FROM repomesh_secrets.versions WHERE purpose IN ('model-provider-key','operation-input')`

	status, data, _ := request("GET", "/api/model-providers", "", "", "", "")
	wantError(status, data, 401, "AUTHENTICATION_REQUIRED")
	status, _, _ = request("GET", "/__test/login?actor=a", "", "", "", "")
	if status != http.StatusSeeOther {
		t.Fatalf("test login status=%d", status)
	}
	status, data, _ = request("GET", "/api/session", "", "", "", "")
	var session access.Session
	if status != 200 || json.Unmarshal(data, &session) != nil || session.User.ID != server.fixtures.ActorA {
		t.Fatalf("session status=%d body=%s", status, data)
	}
	origin, csrf := server.server.URL, session.CSRFToken

	const key = "70000000-0000-4000-8000-000000000001"
	const body = `{"providerId":null,"expectedRevision":null,"name":"开发网关","baseUrl":"https://gateway.example.invalid/v1","apiFormat":"openai_chat_completions","secret":{"mode":"replace","value":"sk-test-key"},"models":[{"id":null,"modelId":"deepseek-chat","displayName":"对话","contextWindow":256000,"maxOutputTokens":128000,"reasoning":true,"vision":false}]}`
	otherKeyBody := strings.Replace(body, "sk-test-key", "sk-other-key", 1)

	status, data, _ = request("POST", "/api/model-provider-saves", body, "", csrf, key)
	wantError(status, data, 403, "ORIGIN_REJECTED")
	status, data, _ = request("POST", "/api/model-provider-saves", body, "https://evil.invalid", csrf, key)
	wantError(status, data, 403, "ORIGIN_REJECTED")
	status, data, _ = request("POST", "/api/model-provider-saves", body, origin, "", key)
	wantError(status, data, 403, "CSRF_REJECTED")
	status, data, _ = request("POST", "/api/model-provider-saves", body, origin, csrf, "")
	wantError(status, data, 400, "INVALID_IDEMPOTENCY_KEY")
	status, data, _ = send(client, "POST", "/api/model-provider-saves", body, "text/plain", origin, csrf, key)
	wantError(status, data, 415, "UNSUPPORTED_MEDIA_TYPE")
	status, data, _ = request("POST", "/api/model-provider-saves", strings.Replace(body, `{"mode":"replace","value":"sk-test-key"}`, `{"mode":"keep"}`, 1), origin, csrf, key)
	failed := wantError(status, data, 422, "VALIDATION_FAILED")
	if len(failed.Error.FieldErrors) != 1 || failed.Error.FieldErrors[0].Field != "secret" || failed.Error.FieldErrors[0].Code != "REQUIRED" {
		t.Fatalf("validation field errors=%+v", failed.Error.FieldErrors)
	}
	if count(`SELECT count(*) FROM repomesh_models.save_operations`) != 0 || count(`SELECT count(*) FROM repomesh_models.providers`) != 0 || count(modelSecrets) != 0 {
		t.Fatal("rejected requests left rows behind")
	}

	statuses := make(chan int, 20)
	receipts := make(chan models.CommittedSave, 20)
	var workers sync.WaitGroup
	for range 20 {
		workers.Go(func() {
			code, data, _ := request("POST", "/api/model-provider-saves", body, origin, csrf, key)
			statuses <- code
			var receipt models.CommittedSave
			if json.Unmarshal(data, &receipt) == nil {
				receipts <- receipt
			}
		})
	}
	workers.Wait()
	close(statuses)
	close(receipts)
	created, replayed := 0, 0
	for code := range statuses {
		switch code {
		case 201:
			created++
		case 200:
			replayed++
		default:
			t.Fatalf("concurrent save status=%d", code)
		}
	}
	if created != 1 || replayed != 19 {
		t.Fatalf("concurrent save created=%d replayed=%d", created, replayed)
	}
	var receipt models.CommittedSave
	for candidate := range receipts {
		if receipt.SaveID == "" {
			receipt = candidate
		}
		if candidate.SaveID != key || candidate.Outcome != "committed" || candidate.ProviderID != receipt.ProviderID || candidate.ProviderRevision != receipt.ProviderRevision || candidate.SecretVersionID != receipt.SecretVersionID || !candidate.CommittedAt.Equal(receipt.CommittedAt) {
			t.Fatalf("concurrent receipts differ: %+v vs %+v", candidate, receipt)
		}
	}
	if receipt.Links.Provider != "/api/model-providers/"+receipt.ProviderID || receipt.Links.Operation != "/api/model-provider-saves/"+key {
		t.Fatalf("receipt links=%+v", receipt.Links)
	}
	if count(`SELECT count(*) FROM repomesh_models.providers`) != 1 || count(`SELECT count(*) FROM repomesh_models.provider_revisions`) != 1 || count(modelSecrets) != 2 {
		t.Fatal("concurrent saves duplicated provider or secret rows")
	}

	status, data, _ = request("POST", "/api/model-provider-saves", body, origin, csrf, strings.ToUpper(key))
	var replay models.CommittedSave
	if status != 200 || json.Unmarshal(data, &replay) != nil || replay.ProviderRevision != receipt.ProviderRevision || replay.SaveID != key {
		t.Fatalf("uppercase replay status=%d body=%s", status, data)
	}
	status, data, header := request("GET", "/api/model-provider-saves/"+key, "", "", "", "")
	if status != 200 || header.Get("Cache-Control") != "no-store" || json.Unmarshal(data, &replay) != nil || replay.ProviderRevision != receipt.ProviderRevision || !replay.CommittedAt.Equal(receipt.CommittedAt) {
		t.Fatalf("save lookup status=%d body=%s", status, data)
	}
	status, data, _ = request("GET", "/api/model-provider-saves/70000000-0000-4000-8000-0000000000ff", "", "", "", "")
	wantError(status, data, 404, "MODEL_SAVE_NOT_FOUND")
	status, data, _ = request("POST", "/api/model-provider-saves", otherKeyBody, origin, csrf, key)
	wantError(status, data, 409, "IDEMPOTENCY_CONFLICT")

	status, data, _ = request("GET", "/api/model-providers/"+receipt.ProviderID, "", "", "", "")
	var view models.ProviderView
	if status != 200 || json.Unmarshal(data, &view) != nil {
		t.Fatalf("provider status=%d body=%s", status, data)
	}
	if view.ID != receipt.ProviderID || view.Revision != receipt.ProviderRevision || view.Name != "开发网关" || view.BaseURL != "https://gateway.example.invalid/v1" || view.APIFormat != "openai_chat_completions" {
		t.Fatalf("provider view=%+v", view)
	}
	if !view.Secret.Configured || view.Secret.VersionID == nil || *view.Secret.VersionID != receipt.SecretVersionID || view.Secret.Availability != "available" {
		t.Fatalf("provider secret view=%+v", view.Secret)
	}
	if len(view.Models) != 1 || view.Models[0].ModelID != "deepseek-chat" || view.Models[0].DisplayName != "对话" || view.Models[0].ContextWindow != 256000 || view.Models[0].MaxOutputTokens != 128000 || !view.Models[0].Reasoning || view.Models[0].Vision || view.Models[0].ModelProfileID == "" {
		t.Fatalf("provider models=%+v", view.Models)
	}
	status, data, _ = request("GET", "/api/model-providers/"+receipt.ProviderID+"/versions/"+receipt.ProviderRevision, "", "", "", "")
	var version models.ProviderView
	if status != 200 || json.Unmarshal(data, &version) != nil || version.Revision != receipt.ProviderRevision || len(version.Models) != 1 {
		t.Fatalf("provider version status=%d body=%s", status, data)
	}
	status, data, _ = request("GET", "/api/model-providers/"+receipt.ProviderID+"/versions/70000000-0000-4000-8000-0000000000ff", "", "", "", "")
	wantError(status, data, 404, "RESOURCE_NOT_FOUND")
	status, data, _ = request("GET", "/api/model-providers/not-a-provider", "", "", "", "")
	wantError(status, data, 404, "RESOURCE_NOT_FOUND")
	status, data, _ = request("GET", "/api/model-providers?limit=10", "", "", "", "")
	var page models.ProviderPage
	if status != 200 || json.Unmarshal(data, &page) != nil || len(page.Items) != 1 || page.Items[0].ID != receipt.ProviderID || page.Items[0].Revision != receipt.ProviderRevision || page.Items[0].ModelCount != 1 || page.NextCursor != nil {
		t.Fatalf("provider list status=%d body=%s", status, data)
	}
	status, data, _ = request("GET", "/api/model-providers?limit=0", "", "", "", "")
	wantError(status, data, 422, "VALIDATION_FAILED")

	const closeKey = "70000000-0000-4000-8000-000000000002"
	status, data, _ = request("POST", "/api/model-provider-saves/"+closeKey+"/close", "{}", "", csrf, closeKey)
	wantError(status, data, 403, "ORIGIN_REJECTED")
	status, data, _ = request("POST", "/api/model-provider-saves/"+closeKey+"/close", "{}", origin, csrf, key)
	wantError(status, data, 400, "INVALID_IDEMPOTENCY_KEY")
	status, data, _ = request("POST", "/api/model-provider-saves/"+closeKey+"/close", "{}", origin, csrf, closeKey)
	var closed models.ClosedSave
	if status != 200 || json.Unmarshal(data, &closed) != nil || closed.SaveID != closeKey || closed.Outcome != "closed_without_save" || closed.Links.Operation != "/api/model-provider-saves/"+closeKey {
		t.Fatalf("close empty slot status=%d body=%s", status, data)
	}
	status, data, _ = request("POST", "/api/model-provider-saves", body, origin, csrf, closeKey)
	wantError(status, data, 409, "MODEL_SAVE_CLOSED")
	status, data, _ = request("POST", "/api/model-provider-saves/"+closeKey+"/close", "{}", origin, csrf, closeKey)
	var closedAgain models.ClosedSave
	if status != 200 || json.Unmarshal(data, &closedAgain) != nil || !closedAgain.ClosedAt.Equal(closed.ClosedAt) {
		t.Fatalf("second close status=%d body=%s", status, data)
	}
	status, data, _ = request("GET", "/api/model-provider-saves/"+closeKey, "", "", "", "")
	if status != 200 || json.Unmarshal(data, &closedAgain) != nil || closedAgain.Outcome != "closed_without_save" {
		t.Fatalf("closed slot lookup status=%d body=%s", status, data)
	}
	if count(`SELECT count(*) FROM repomesh_models.providers`) != 1 || count(modelSecrets) != 2 {
		t.Fatal("late save after close wrote provider or secret rows")
	}
	status, data, _ = request("POST", "/api/model-provider-saves/"+key+"/close", "{}", origin, csrf, key)
	var closedCommitted models.CommittedSave
	if status != 200 || json.Unmarshal(data, &closedCommitted) != nil || closedCommitted.Outcome != "committed" || closedCommitted.ProviderRevision != receipt.ProviderRevision {
		t.Fatalf("close after save status=%d body=%s", status, data)
	}

	other := newClient()
	status, _, _ = send(other, "GET", "/__test/login?actor=b", "", "", "", "", "")
	if status != http.StatusSeeOther {
		t.Fatalf("other actor login status=%d", status)
	}
	status, data, _ = send(other, "GET", "/api/model-provider-saves/"+key, "", "", "", "", "")
	wantError(status, data, 404, "MODEL_SAVE_NOT_FOUND")
	status, data, _ = send(other, "GET", "/api/model-providers/"+receipt.ProviderID, "", "", "", "", "")
	wantError(status, data, 404, "RESOURCE_NOT_FOUND")
	status, data, _ = send(other, "GET", "/api/model-providers", "", "", "", "", "")
	if status != 200 || json.Unmarshal(data, &page) != nil || len(page.Items) != 0 {
		t.Fatalf("other actor provider list status=%d body=%s", status, data)
	}

	seen.Lock()
	defer seen.Unlock()
	for _, response := range bodies {
		if strings.Contains(response, "sk-test-key") || strings.Contains(response, "sk-other-key") {
			t.Fatalf("business key echoed in a response: %s", response)
		}
	}
}

var errSaveResponseReachedClient = errors.New("model save HTTP response reached the client")

func holdSaveInsert(t *testing.T, pool *pgxpool.Pool) func() {
	t.Helper()
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `CREATE OR REPLACE FUNCTION repomesh_models.hold_save_insert() RETURNS trigger
		LANGUAGE plpgsql AS $$ BEGIN PERFORM pg_advisory_xact_lock(4770304); RETURN NEW; END $$;
		DROP TRIGGER IF EXISTS hold_save_insert ON repomesh_models.save_operations;
		CREATE TRIGGER hold_save_insert BEFORE INSERT ON repomesh_models.save_operations
		FOR EACH ROW EXECUTE FUNCTION repomesh_models.hold_save_insert()`); err != nil {
		t.Fatal(err)
	}
	connection, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := connection.Exec(ctx, `SELECT pg_advisory_lock(4770304)`); err != nil {
		connection.Release()
		t.Fatal(err)
	}
	var once sync.Once
	release := func() {
		once.Do(func() {
			_, _ = connection.Exec(context.Background(), `SELECT pg_advisory_unlock(4770304)`)
			connection.Release()
		})
	}
	t.Cleanup(func() {
		release()
		_, _ = pool.Exec(context.Background(), `DROP TRIGGER IF EXISTS hold_save_insert ON repomesh_models.save_operations;
			DROP FUNCTION IF EXISTS repomesh_models.hold_save_insert()`)
	})
	return release
}

func TestPostgresModelSaveLostResponseAndRestart(t *testing.T) {
	assets := t.TempDir()
	if err := os.WriteFile(filepath.Join(assets, "index.html"), []byte("<!doctype html><title>B04 S07</title>"), 0600); err != nil {
		t.Fatal(err)
	}
	server := startProjectBrowserServer(t, assets)
	jar, _ := cookiejar.New(nil)
	client := server.server.Client()
	client.Jar = jar
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	get := func(base *httptest.Server, using *http.Client, path string) (int, []byte) {
		t.Helper()
		req, err := http.NewRequest(http.MethodGet, base.URL+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		response, err := using.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		data, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}
		return response.StatusCode, data
	}
	status, _ := get(server.server, client, "/__test/login?actor=a")
	if status != http.StatusSeeOther {
		t.Fatalf("login status=%d", status)
	}
	status, data := get(server.server, client, "/api/session")
	var session access.Session
	if status != 200 || json.Unmarshal(data, &session) != nil {
		t.Fatalf("session status=%d body=%s", status, data)
	}
	const key = "71000000-0000-4000-8000-000000000001"
	const body = `{"providerId":null,"expectedRevision":null,"name":"开发网关","baseUrl":"https://gateway.example.invalid/v1","apiFormat":"openai_chat_completions","secret":{"mode":"replace","value":"sk-lost-key"},"models":[{"id":null,"modelId":"deepseek-chat","displayName":"对话","contextWindow":256000,"maxOutputTokens":128000,"reasoning":true,"vision":false}]}`
	status, data = get(server.server, client, "/api/model-provider-saves/"+key)
	if status != 404 || !strings.Contains(string(data), "MODEL_SAVE_NOT_FOUND") {
		t.Fatalf("GET before save status=%d body=%s", status, data)
	}
	release := holdSaveInsert(t, server.pool)
	server.mu.Lock()
	server.dropMethod, server.dropPath = http.MethodPost, "/api/model-provider-saves"
	server.mu.Unlock()
	lost := make(chan error, 1)
	go func() {
		req, err := http.NewRequest(http.MethodPost, server.server.URL+"/api/model-provider-saves", strings.NewReader(body))
		if err != nil {
			lost <- err
			return
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", server.server.URL)
		req.Header.Set("X-CSRF-Token", session.CSRFToken)
		req.Header.Set("Idempotency-Key", key)
		dropClient := server.server.Client()
		dropClient.Jar = jar
		transport := dropClient.Transport.(*http.Transport).Clone()
		transport.DisableKeepAlives = true
		dropClient.Transport = transport
		response, err := dropClient.Do(req)
		if err == nil {
			payload, _ := io.ReadAll(response.Body)
			response.Body.Close()
			lost <- fmt.Errorf("%w status=%d body=%s", errSaveResponseReachedClient, response.StatusCode, payload)
			return
		}
		lost <- err
	}()
	var waiting bool
	deadline := time.Now().Add(3 * time.Second)
	for {
		if err := server.pool.QueryRow(context.Background(), `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event='advisory' AND query LIKE '%save_operations%')`).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !waiting {
		t.Fatal("save insert did not wait on advisory lock 4770304")
	}
	var visible int
	if err := server.pool.QueryRow(context.Background(), `SELECT count(*) FROM repomesh_models.save_operations WHERE save_id=$1`, key).Scan(&visible); err != nil || visible != 0 {
		t.Fatalf("committed save visible before insert finished count=%d", visible)
	}
	release()
	select {
	case err := <-lost:
		if err == nil || errors.Is(err, errSaveResponseReachedClient) {
			t.Fatalf("dropped save response reached the client: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("dropped save did not finish")
	}
	projectService := projects.New(server.pool, server.auth)
	server.auth.SetProjectDestinationResolver(projectService.ResolveDestination)
	modelService := models.New(server.pool, server.auth, server.store, projects.NewCatalogWriter())
	server.auth.SetModelSaveDestinationResolver(modelService.ResolveDestination)
	restarted := httptest.NewUnstartedServer(nil)
	restartedOrigin := "https://" + restarted.Listener.Addr().String()
	restarted.Config.Handler = server.wrapProduct(handlerConfigured(os.DirFS(assets), Auth{Service: server.auth, Origin: restartedOrigin}, Projects{Service: projectService}, Models{Service: modelService}, Scan{}, Decision{}, Skills{}, Issues{}, Messages{}, AgentTeams{}, Pipeline{}, HumanControl{}, ObserveV1{}, Discovery{}, Console{}))
	restarted.StartTLS()
	t.Cleanup(restarted.Close)
	restartedJar, _ := cookiejar.New(nil)
	restartedClient := restarted.Client()
	restartedClient.Jar = restartedJar
	restartedClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	status, _ = get(restarted, restartedClient, "/__test/login?actor=a")
	if status != http.StatusSeeOther {
		t.Fatalf("restart login status=%d", status)
	}
	status, data = get(restarted, restartedClient, "/api/session")
	var restartedSession access.Session
	if status != 200 || json.Unmarshal(data, &restartedSession) != nil {
		t.Fatalf("restart session status=%d body=%s", status, data)
	}
	status, data = get(restarted, restartedClient, "/api/model-provider-saves/"+key)
	var receipt models.CommittedSave
	if status != 200 || json.Unmarshal(data, &receipt) != nil || receipt.SaveID != key || receipt.Outcome != "committed" || receipt.ProviderID == "" {
		t.Fatalf("restart GET status=%d body=%s", status, data)
	}
	var providers, operations int
	if err := server.pool.QueryRow(context.Background(), `SELECT count(*) FROM repomesh_models.providers`).Scan(&providers); err != nil || providers != 1 {
		t.Fatalf("providers=%d", providers)
	}
	if err := server.pool.QueryRow(context.Background(), `SELECT count(*) FROM repomesh_models.save_operations WHERE actor=$1 AND save_id=$2`, server.fixtures.ActorA, key).Scan(&operations); err != nil || operations != 1 {
		t.Fatalf("operations=%d", operations)
	}
	req, err := http.NewRequest(http.MethodPost, restarted.URL+"/api/model-provider-saves/"+key+"/close", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", restarted.URL)
	req.Header.Set("X-CSRF-Token", restartedSession.CSRFToken)
	req.Header.Set("Idempotency-Key", key)
	response, err := restartedClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	closeBody, _ := io.ReadAll(response.Body)
	response.Body.Close()
	var closed models.CommittedSave
	if response.StatusCode != 200 || json.Unmarshal(closeBody, &closed) != nil || closed.Outcome != "committed" || closed.ProviderRevision != receipt.ProviderRevision {
		t.Fatalf("close after lost save status=%d body=%s", response.StatusCode, closeBody)
	}
	late, err := http.NewRequest(http.MethodPost, restarted.URL+"/api/model-provider-saves", strings.NewReader(strings.Replace(body, "sk-lost-key", "sk-other-key", 1)))
	if err != nil {
		t.Fatal(err)
	}
	late.Header.Set("Content-Type", "application/json")
	late.Header.Set("Origin", restarted.URL)
	late.Header.Set("X-CSRF-Token", restartedSession.CSRFToken)
	late.Header.Set("Idempotency-Key", key)
	lateResponse, err := restartedClient.Do(late)
	if err != nil {
		t.Fatal(err)
	}
	lateBody, _ := io.ReadAll(lateResponse.Body)
	lateResponse.Body.Close()
	if lateResponse.StatusCode != 409 || !strings.Contains(string(lateBody), "IDEMPOTENCY_CONFLICT") {
		t.Fatalf("late save status=%d body=%s", lateResponse.StatusCode, lateBody)
	}
	if strings.Contains(string(data)+string(closeBody)+string(lateBody), "sk-lost-key") {
		t.Fatal("lost-save key echoed after restart")
	}
}
