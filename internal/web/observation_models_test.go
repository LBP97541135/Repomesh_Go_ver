package web

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"repomesh.local/repomesh/internal/access"
	"repomesh.local/repomesh/internal/observeui"
)

func TestObservationModelBridgeStaysOnLoopback(t *testing.T) {
	for _, invalid := range []string{"https://example.org", "http://127.0.0.1:1234/anything", "http://user:key@127.0.0.1", "http://127.0.0.1?target=x", "http://127.0.0.1/#x", "file:///tmp/key"} {
		if _, err := NewObservationModels(invalid); err == nil {
			t.Fatalf("accepted invalid origin: %s", invalid)
		}
	}
	var visited bool
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { visited = true }))
	defer destination.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, destination.URL, 307) }))
	defer redirect.Close()
	bridge, err := NewObservationModels(redirect.URL)
	if err != nil {
		t.Fatal(err)
	}
	var output any
	if bridge.request(context.Background(), "PUT", "/api/model", map[string]string{"api_key": "private-test-key"}, &output) == nil || visited {
		t.Fatal("configuration followed a redirect")
	}
}

func TestPostgresObservationSettingsSaveToActualWorkbench(t *testing.T) {
	archive := filepath.Join(t.TempDir(), "archive")
	workbench, err := observeui.New(observeui.Options{Archive: archive})
	if err != nil {
		t.Fatal(err)
	}
	local := httptest.NewServer(workbench)
	defer local.Close()
	bridge, err := NewObservationModels(local.URL)
	if err != nil {
		t.Fatal(err)
	}
	assets := t.TempDir()
	if err := os.WriteFile(filepath.Join(assets, "index.html"), []byte("settings fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	server := startProjectBrowserServerWithOptions(t, assets, browserFixtureOptions{Observation: bridge})
	jar, _ := cookiejar.New(nil)
	client := server.server.Client()
	client.Jar = jar
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	send := func(method, path, body, origin, csrf string) (int, []byte) {
		t.Helper()
		req, _ := http.NewRequest(method, server.server.URL+path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", origin)
		req.Header.Set("X-CSRF-Token", csrf)
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		data, _ := io.ReadAll(res.Body)
		if strings.Contains(string(data), "settings-secret") {
			t.Fatal("key leaked into the settings response")
		}
		return res.StatusCode, data
	}
	path := "/api/settings/observation-models/jev"
	if status, _ := send("GET", path, "", "", ""); status != 401 {
		t.Fatal("anonymous settings request accepted")
	}
	send("GET", "/__test/login?actor=a", "", "", "")
	_, data := send("GET", "/api/session", "", "", "")
	var session access.Session
	if json.Unmarshal(data, &session) != nil {
		t.Fatal("invalid session")
	}
	if status, _ := send("GET", path, "", "", ""); status != 403 {
		t.Fatal("non-admin global configuration access accepted")
	}
	if _, err := server.pool.Exec(context.Background(), `UPDATE repomesh_access.accounts SET is_admin=true WHERE id=$1`, server.fixtures.ActorA); err != nil {
		t.Fatal(err)
	}
	config := `{"model":"jev-1.13.0","api_key":"jev-settings-secret"}`
	if status, _ := send("POST", path, config, "", session.CSRFToken); status != 403 {
		t.Fatal("missing origin accepted")
	}
	if status, _ := send("POST", path, config, server.server.URL, ""); status != 403 {
		t.Fatal("missing CSRF accepted")
	}
	if status, _ := send("POST", path, config, server.server.URL, session.CSRFToken); status != 200 {
		t.Fatal("authenticated settings save failed")
	}
	if status, _ := send("POST", path, `{"model":"jev-latest","api_key":""}`, server.server.URL, session.CSRFToken); status != 200 {
		t.Fatal("model update with blank key failed")
	}
	if status, _ := send("POST", "/api/settings/observation-models/deepseek", `{"model":"deepseek-flash","api_key":"deepseek-settings-secret"}`, server.server.URL, session.CSRFToken); status != 200 {
		t.Fatal("DeepSeek save failed")
	}
	if status, _ := send("POST", "/api/settings/observation-models/arbitrary", `{}`, server.server.URL, session.CSRFToken); status != 404 {
		t.Fatal("unlisted model purpose accepted")
	}
	for _, tc := range []struct{ purpose, file, model, key string }{
		{"jev", "model.json", "jev-latest", "jev-settings-secret"},
		{"deepseek", "assistant.json", "deepseek-flash", "deepseek-settings-secret"},
	} {
		status, body := send("GET", "/api/settings/observation-models/"+tc.purpose, "", "", "")
		if status != 200 || !strings.Contains(string(body), tc.model) || !strings.Contains(string(body), `"configured":true`) {
			t.Fatal("configuration did not reload")
		}
		stored, err := os.ReadFile(filepath.Join(archive, tc.file))
		if err != nil || !strings.Contains(string(stored), tc.key) || !strings.Contains(string(stored), tc.model) {
			t.Fatal("settings did not update the actual private model configuration")
		}
		info, _ := os.Stat(filepath.Join(archive, tc.file))
		if info.Mode().Perm() != 0600 {
			t.Fatal("model configuration permissions are not private")
		}
	}
}

// Explicit browser fixture: real session/CSRF/admin routes and an explicitly
// selected real workbench; the surrounding account/catalog are test data.
func TestObservationSettingsBrowserServer(t *testing.T) {
	if os.Getenv("REPOMESH_OBSERVE_SETTINGS_BROWSER") != "1" {
		t.Skip("observation settings browser fixture disabled")
	}
	bridge, err := NewObservationModels(os.Getenv("REPOMESH_OBSERVE_WORKBENCH_URL"))
	if err != nil {
		t.Fatal(err)
	}
	assets := os.Getenv("REPOMESH_B03_BROWSER_ASSETS")
	output := os.Getenv("REPOMESH_B03_BROWSER_MANIFEST_DIR")
	duration, err := time.ParseDuration(os.Getenv("REPOMESH_B03_BROWSER_DURATION"))
	if assets == "" || output == "" || err != nil || duration <= 0 || duration > 30*time.Minute {
		t.Fatal("explicit assets, private manifest directory and bounded duration required")
	}
	server := startProjectBrowserServerWithOptions(t, assets, browserFixtureOptions{Observation: bridge})
	if _, err := server.pool.Exec(context.Background(), `UPDATE repomesh_access.accounts SET is_admin=true WHERE id=$1`, server.fixtures.ActorA); err != nil {
		t.Fatal(err)
	}
	if err := writeBrowserManifest(output, server.manifest()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-server.shutdown:
	case <-time.After(duration):
	}
}
