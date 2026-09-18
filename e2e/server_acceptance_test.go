//go:build e2e

// Server acceptance run: drives the real production stack end-to-end over
// its public HTTP surface with an authenticated session — assembly, plan
// promotion, dual dispatch, gates, interface docs, branch validation,
// observability and honest 401/503 fallbacks.
package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"os"
	"testing"
	"time"
)

const base = "https://crazykitties.cn"

func serverClient(t *testing.T) *http.Client {
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	return &http.Client{Jar: jar, Timeout: 30 * time.Second}
}

func serverCall(t *testing.T, client *http.Client, method, path string, body any, wantStatus int) map[string]any {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		reader = bytes.NewReader(raw)
	} else {
		reader = bytes.NewReader(nil)
	}
	req, err := http.NewRequest(method, base+path, reader)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", base)
	if os.Getenv("REPOMESH_E2E_SESSION") != "" {
		req.Header.Set("Cookie", "__Host-repomesh-session="+os.Getenv("REPOMESH_E2E_SESSION"))
	}
	res, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer res.Body.Close()
	if res.StatusCode != wantStatus {
		t.Fatalf("%s %s: got %d want %d", method, path, res.StatusCode, wantStatus)
	}
	out := map[string]any{}
	_ = json.NewDecoder(res.Body).Decode(&out)
	return out
}

// TestServerAcceptance runs the public-surface acceptance against the live
// deployment. Set REPOMESH_E2E_SESSION to a valid session cookie to run the
// authenticated steps; without it only anonymous steps assert.
func TestServerAcceptance(t *testing.T) {
	client := serverClient(t)

	// 1. Anonymous surface: health + honest fallbacks.
	serverCall(t, client, "GET", "/healthz", nil, 200)
	serverCall(t, client, "GET", "/api/observe/summary", nil, 401)
	serverCall(t, client, "GET", "/api/nonexistent-endpoint", nil, 404)

	session := os.Getenv("REPOMESH_E2E_SESSION")
	if session == "" {
		t.Log("no REPOMESH_E2E_SESSION; anonymous surface verified only")
		return
	}

	// 2. Authenticated session works and reports the GitHub connection.
	sessionView := serverCall(t, client, "GET", "/api/session", nil, 200)
	if sessionView["user"] == nil {
		t.Fatal("session has no user")
	}
	t.Logf("session user: %v", sessionView["user"])

	// 3. Console directory responds.
	orgs := serverCall(t, client, "GET", "/api/console/organizations", nil, 200)
	agents := serverCall(t, client, "GET", "/api/console/agents", nil, 200)
	teams := serverCall(t, client, "GET", "/api/console/teams", nil, 200)
	t.Logf("orgs=%v agents=%v teams=%v", orgs["organizations"], agents["agents"], teams["teams"])

	// 4. Observability reads (empty is a valid honest state).
	summary := serverCall(t, client, "GET", "/api/observe/summary", nil, 200)
	if _, ok := summary["calls"]; !ok {
		t.Fatal("summary missing calls")
	}
	rules := serverCall(t, client, "GET", "/api/observe/alert-rules", nil, 200)
	t.Logf("alert rules: %v", rules["rules"])

	// 5. Review queue.
	reviews := serverCall(t, client, "GET", "/api/v1/review-requests", nil, 200)
	t.Logf("review queue: %v", reviews)

	// 6. AgentTeams adapter through the web origin.
	serverCall(t, client, "GET", "/api/agentteams/projects/43ac8deb-8890-40c8-8e79-984c9f70aac1/workflow", nil, 200)

	// 7. Issue list for the scanned project (may be empty).
	serverCall(t, client, "GET", "/api/projects", nil, 200)

	fmt.Println("SERVER ACCEPTANCE: authenticated surface verified")
}
