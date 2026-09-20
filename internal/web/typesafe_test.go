package web

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"repomesh.local/repomesh/internal/access"
	"repomesh.local/repomesh/internal/secrets"
	"repomesh.local/repomesh/internal/testdb"
	"repomesh.local/repomesh/internal/typesafe"
)

type typeSafeFixture struct {
	pool         *pgxpool.Pool
	project      testdb.ProjectFixture
	service      *typesafe.Service
	server       *httptest.Server
	cookie, csrf string
}

func newTypeSafeFixture(t *testing.T) *typeSafeFixture {
	t.Helper()
	ctx := context.Background()
	f := &typeSafeFixture{pool: testdb.Open(t)}
	f.project = testdb.SeedProject(t, f.pool, "", "", "fixture/repository")
	testdb.SeedIssue(t, f.pool, f.project, "typesafe-web-issue", "fixture/repository")
	root := make([]byte, 32)
	_, _ = rand.Read(root)
	file := filepath.Join(t.TempDir(), "root")
	if err := os.WriteFile(file, root, 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := secrets.New(ctx, f.pool, secrets.Config{ActiveRootID: "web-typesafe", Roots: []secrets.RootFile{{ID: "web-typesafe", Path: file}}})
	if err != nil {
		t.Fatal(err)
	}
	auth := access.New(f.pool, store, nil)
	var random [32]byte
	_, _ = rand.Read(random[:])
	f.cookie = base64.RawURLEncoding.EncodeToString(random[:])
	sessionHash := sha256.Sum256([]byte(f.cookie))
	bindingHash := sha256.Sum256([]byte("binding:" + f.cookie))
	_, err = f.pool.Exec(ctx, `INSERT INTO repomesh_access.bindings(hash,identity_generation,expires_at) VALUES($1,1,now()+interval '1 day')`, hex.EncodeToString(bindingHash[:]))
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.pool.Exec(ctx, `INSERT INTO repomesh_access.sessions(hash,actor,binding,generation,expires_at,last_active_at)
		VALUES($1,$2,$3,1,now()+interval '1 day',now())`, hex.EncodeToString(sessionHash[:]), f.project.OwnerID, hex.EncodeToString(bindingHash[:]))
	if err != nil {
		t.Fatal(err)
	}
	session, err := auth.Session(ctx, f.cookie)
	if err != nil {
		t.Fatal(err)
	}
	f.csrf = session.CSRFToken
	f.service = typesafe.New(f.pool, store)
	mux := http.NewServeMux()
	registerTypeSafe(mux, Auth{Service: auth, Origin: "https://repomesh.test"}, f.service)
	f.server = httptest.NewServer(mux)
	t.Cleanup(f.server.Close)
	return f
}
func (f *typeSafeFixture) request(t *testing.T, method, path string, body any, authorized bool) (int, []byte) {
	t.Helper()
	data, _ := json.Marshal(body)
	r, err := http.NewRequest(method, f.server.URL+path, bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	r.Header.Set("Content-Type", "application/json")
	if authorized {
		r.AddCookie(&http.Cookie{Name: sessionCookie, Value: f.cookie})
		r.Header.Set("Origin", "https://repomesh.test")
		r.Header.Set("X-CSRF-Token", f.csrf)
	}
	res, err := f.server.Client().Do(r)
	if err != nil {
		t.Fatal("local HTTP request failed")
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return res.StatusCode, raw
}

func TestTypeSafeSettingsHTTPGuardsAndNoKeyEcho(t *testing.T) {
	f := newTypeSafeFixture(t)
	path := "/api/projects/" + f.project.ID + "/typesafe"
	status, _ := f.request(t, "GET", path, nil, false)
	if status != 401 {
		t.Fatal("anonymous read accepted")
	}
	input := map[string]any{"expectedRevision": 0, "enabled": true, "model": typesafe.DefaultModel, "secret": map[string]string{"mode": "replace", "value": "fixture-web-typesafe-key"}}
	status, raw := f.request(t, "POST", path, input, true)
	if status != 200 {
		t.Fatalf("save HTTP status %d: %s", status, raw)
	}
	if strings.Contains(string(raw), "fixture-web-typesafe-key") {
		t.Fatal("POST echoed key")
	}
	status, raw = f.request(t, "GET", path, nil, true)
	if status != 200 || strings.Contains(string(raw), "fixture-web-typesafe-key") {
		t.Fatal("GET exposed credential")
	}
	status, _ = f.request(t, "POST", path, input, true)
	if status != 409 {
		t.Fatal("stale browser save was accepted")
	}
	other := testdb.SeedProject(t, f.pool, "", "", "fixture/other")
	status, _ = f.request(t, "GET", "/api/projects/"+other.ID+"/typesafe", nil, true)
	if status != 404 {
		t.Fatal("foreign project visible")
	}
	status, _ = f.request(t, "POST", "/api/typesafe/evaluations", map[string]any{}, true)
	if status != 401 {
		t.Fatal("browser cookie substituted for run grant")
	}
	for _, omit := range []string{"Origin", "X-CSRF-Token"} {
		data, _ := json.Marshal(input)
		r, _ := http.NewRequest("POST", f.server.URL+path, bytes.NewReader(data))
		r.AddCookie(&http.Cookie{Name: sessionCookie, Value: f.cookie})
		r.Header.Set("Origin", "https://repomesh.test")
		r.Header.Set("X-CSRF-Token", f.csrf)
		r.Header.Del(omit)
		res, err := f.server.Client().Do(r)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != 403 {
			t.Fatalf("missing %s accepted", omit)
		}
	}
}

func (f *typeSafeFixture) grant(t *testing.T, runID, kind string) *typesafe.Grant {
	t.Helper()
	ctx := context.Background()
	_, err := f.pool.Exec(ctx, `INSERT INTO repomesh_execution.workers(id,host,kind) VALUES('web-typesafe-worker','fixture','host_executor') ON CONFLICT DO NOTHING`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.pool.Exec(ctx, `INSERT INTO repomesh_execution.attempts(id,project_id,issue_id,worker_id,configuration_revision,state,reservation_generation)
		VALUES($1,$2,'typesafe-web-issue','web-typesafe-worker',$3,'running',(SELECT COALESCE(MAX(reservation_generation),0)+1 FROM repomesh_execution.attempts WHERE project_id=$2))`, runID+"-attempt", f.project.ID, f.project.ConfigurationID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.pool.Exec(ctx, `INSERT INTO repomesh_execution.agent_runs(id,attempt_id,agent_kind,command,workspace,task_package_ref,state,pid,started_at)
		VALUES($1,$2,$3,'live-verification','/temporary','fixture-task','running',1,now())`, runID, runID+"-attempt", kind)
	if err != nil {
		t.Fatal(err)
	}
	g, err := typesafe.IssueGrant(ctx, f.pool, "web-typesafe-worker", runID)
	if err != nil || g == nil {
		t.Fatal("could not issue scoped grant")
	}
	return g
}
