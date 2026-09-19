package web

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"repomesh.local/repomesh/internal/access"
	"repomesh.local/repomesh/internal/github"
	"repomesh.local/repomesh/internal/secrets"
	"repomesh.local/repomesh/internal/testdb"
)

type httpProvider struct{}

func TestUnconfiguredAuthReturnsUnavailable(t *testing.T) {
	handler := newHandler(fstest.MapFS{"index.html": {Data: []byte("index")}})
	for _, endpoint := range []struct{ method, path string }{{"GET", "/api/session"}, {"GET", "/api/repositories"}, {"POST", "/api/auth/github/login"}, {"POST", "/api/auth/github/reconnect"}, {"POST", "/api/auth/logout"}} {
		request := httptest.NewRequest(endpoint.method, endpoint.path, strings.NewReader("{}"))
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != 503 {
			t.Fatalf("%s returned %d", endpoint.path, response.Code)
		}
	}
}

func (httpProvider) AuthorizationURL(state, challenge string) string {
	return "https://github.com/login/oauth/authorize?state=" + state + "&code_challenge=" + challenge
}
func (httpProvider) Exchange(context.Context, string, string) (github.TokenSet, error) {
	return github.TokenSet{AccessToken: "http-test-token"}, nil
}
func (httpProvider) Refresh(context.Context, string) (github.TokenSet, error) {
	return github.TokenSet{}, context.DeadlineExceeded
}
func (httpProvider) Identity(context.Context, string) (github.Identity, error) {
	return github.Identity{ID: 55, DisplayName: "Browser test"}, nil
}
func (httpProvider) Repositories(context.Context, string, int) (github.RepositoryPage, error) {
	return github.RepositoryPage{}, nil
}
func (httpProvider) Repository(context.Context, string, string, string) (github.Repository, error) {
	return github.Repository{}, context.DeadlineExceeded
}
func (httpProvider) AppCapability(context.Context, string, string) (github.Capability, error) {
	return github.Capability{}, context.DeadlineExceeded
}
func (httpProvider) ObserveAppInstallation(context.Context, string, string) (github.AppInstallationObservation, error) {
	return github.AppInstallationObservation{}, context.DeadlineExceeded
}

func TestPostgresHTTPAuthenticationBoundary(t *testing.T) {
	pool := testdb.Open(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	root := make([]byte, 32)
	rand.Read(root)
	path := filepath.Join(t.TempDir(), "root.key")
	if err := os.WriteFile(path, root, 0600); err != nil {
		t.Fatal(err)
	}
	store, err := secrets.New(ctx, pool, secrets.Config{ActiveRootID: "http", Roots: []secrets.RootFile{{ID: "http", Path: path}}})
	if err != nil {
		t.Fatal(err)
	}
	service := access.New(pool, store, httpProvider{})
	assets := fstest.MapFS{"index.html": {Data: []byte("<!doctype html><title>RepoMesh</title>")}}
	server := httptest.NewUnstartedServer(handlerWithAuth(assets, Auth{Service: service, Origin: "https://repomesh.test"}))
	server.StartTLS()
	defer server.Close()
	client := server.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	request := func(method, path, body, origin, csrf, key string, cookies ...*http.Cookie) *http.Response {
		t.Helper()
		req, err := http.NewRequest(method, server.URL+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		if method == "POST" {
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Origin", origin)
		}
		if csrf != "" {
			req.Header.Set("X-CSRF-Token", csrf)
		}
		if key != "" {
			req.Header.Set("Idempotency-Key", key)
		}
		for _, cookie := range cookies {
			req.AddCookie(cookie)
		}
		response, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { response.Body.Close() })
		return response
	}
	key := "12345678-1234-4234-8234-123456789abc"
	payload := `{"destination":{"kind":"home"}}`
	response := request("POST", "/api/auth/github/login", payload, "https://evil.test", "", key)
	if response.StatusCode != 403 {
		t.Fatal("cross origin start accepted", response.StatusCode)
	}
	for _, body := range []string{`{"destination":{"kind":"home","kind":"home"}}`, `{"destination":{"kind":"home"},"userId":"admin"}`, `{"destination":{"kind":"project","projectId":"%2e%2e"}}`, `{"destination":{"kind":"operation","operationKind":"conversation_message","operationId":"1","projectId":"2","conversationId":"3"}}`, "  ", `{"destination":{"kind":"project","projectId":"\ud800"}}`} {
		response := request("POST", "/api/auth/github/login", body, "https://repomesh.test", "", key)
		if response.StatusCode != 422 {
			t.Fatalf("invalid input status=%d", response.StatusCode)
		}
	}
	response = request("POST", "/api/auth/github/login", payload, "https://repomesh.test", "", key)
	if response.StatusCode != 201 {
		data, _ := io.ReadAll(response.Body)
		t.Fatalf("start status=%d body=%s", response.StatusCode, data)
	}
	var start access.StartResult
	if err = json.NewDecoder(response.Body).Decode(&start); err != nil {
		t.Fatal(err)
	}
	cookies := response.Cookies()
	if len(cookies) != 1 {
		t.Fatal("missing binding cookie")
	}
	binding := cookies[0]
	if binding.Name != bindingCookie || !binding.Secure || !binding.HttpOnly || binding.Path != "/" || binding.Domain != "" || binding.SameSite != http.SameSiteLaxMode {
		t.Fatal("binding cookie attributes incorrect")
	}
	target, _ := url.Parse(start.AuthorizationURL)
	response = request("GET", "/api/auth/github/callback?code=http-code&state="+target.Query().Get("state"), "", "", "", "", binding)
	if response.StatusCode != 303 || response.Header.Get("Location") != start.ResultPage || response.Header.Get("Referrer-Policy") != "no-referrer" || response.Header.Get("Cache-Control") != "no-store" {
		t.Fatal("callback redirect not fixed and protected")
	}
	cookies = response.Cookies()
	if len(cookies) != 1 {
		t.Fatal("missing session cookie")
	}
	session := cookies[0]
	if session.Name != sessionCookie || !session.Secure || !session.HttpOnly || session.MaxAge != 43200 {
		t.Fatal("session cookie attributes incorrect")
	}
	response = request("GET", "/api/session", "", "", "", "", session)
	var current access.Session
	if response.StatusCode != 200 || json.NewDecoder(response.Body).Decode(&current) != nil || current.User.DisplayName != "Browser test" {
		t.Fatal("session did not expose stable local account")
	}
	response = request("POST", "/api/auth/logout", "{}", "https://repomesh.test", "wrong", "", session)
	if response.StatusCode != 403 {
		t.Fatal("logout accepted wrong CSRF")
	}
	response = request("POST", "/api/auth/logout", "{}", "https://repomesh.test", current.CSRFToken, "", session)
	if response.StatusCode != 204 {
		t.Fatalf("logout status=%d", response.StatusCode)
	}
	deleted := map[string]bool{}
	for _, c := range response.Cookies() {
		if c.Value != "" || c.MaxAge != -1 || c.Path != "/" || !c.Secure || !c.HttpOnly {
			t.Fatalf("logout did not expire cookie %s securely", c.Name)
		}
		deleted[c.Name] = true
	}
	if len(response.Cookies()) != 2 || !deleted[sessionCookie] || !deleted[bindingCookie] {
		t.Fatal("logout must expire both session and binding cookies")
	}
	response = request("GET", "/api/session", "", "", "", "", session)
	if response.StatusCode != 401 {
		t.Fatal("revoked session still authenticated")
	}
	response = request("GET", "/api/auth/github/callback?state=invalid&code=private", "", "", "", "", binding)
	data, _ := io.ReadAll(response.Body)
	if response.StatusCode != 303 || response.Header.Get("Location") != "/login" || bytes.Contains(data, []byte("private")) {
		t.Fatal("invalid callback disclosed input")
	}
	for _, route := range []string{"/login", start.ResultPage} {
		response = request("GET", route, "", "", "", "", binding)
		if response.StatusCode != 200 {
			t.Fatal("SPA route missing", route, response.StatusCode)
		}
	}
}
