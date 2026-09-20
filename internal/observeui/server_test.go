package observeui

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"repomesh.local/repomesh/internal/observepipe"
)

func newTestServer(t *testing.T) (*Server, *httptest.Server) {
	t.Helper()
	s, err := New(Options{Archive: filepath.Join(t.TempDir(), "archive")})
	if err != nil {
		t.Fatal(err)
	}
	h := httptest.NewServer(s)
	t.Cleanup(h.Close)
	return s, h
}
func request(t *testing.T, h *httptest.Server, method, path string, body any) (int, []byte) {
	t.Helper()
	var b []byte
	if body != nil {
		b, _ = json.Marshal(body)
	}
	r, err := http.NewRequest(method, h.URL+path, bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-RepoMesh-Local", "1")
	res, err := h.Client().Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	data, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return res.StatusCode, data
}

func TestLocalSuiteReadbackAndOTLPPersistence(t *testing.T) {
	s, h := newTestServer(t)
	status, b := request(t, h, "POST", "/api/trials", map[string]string{"variant": "suite"})
	if status != 201 {
		t.Fatalf("%d %s", status, b)
	}
	rows, err := observepipe.DatasetRows(s.archives[0].Journal)
	if err != nil || len(rows) != 3 {
		t.Fatalf("rows: %d %v", len(rows), err)
	}
	counts := map[string]int{}
	for _, r := range rows {
		counts[r.Verdict]++
	}
	if counts["pass"] != 1 || counts["fail"] != 1 || counts["unknown"] != 1 {
		t.Fatal(counts)
	}
	t.Setenv("LOCAL_UI_TEST_ENDPOINT", h.URL+"/v1/traces")
	cfg := observepipe.ExportConfig{DestinationID: "test-local-ui", EndpointEnv: "LOCAL_UI_TEST_ENDPOINT", ServiceName: "local-test", TimeoutSeconds: 3}
	result, err := observepipe.ExportOTLP(context.Background(), s.archives[0].Journal, cfg)
	if err != nil || result.Exported != 27 {
		t.Fatalf("OTLP: %+v %v", result, err)
	}
	spans, err := readSpans(s.archives[0].Journal)
	if err != nil || len(spans) != 27 {
		t.Fatalf("spans: %d %v", len(spans), err)
	}
	for _, span := range spans {
		if span.DurationMS != 0 {
			t.Fatal("invented duration")
		}
	}
	result, err = observepipe.ExportOTLP(context.Background(), s.archives[0].Journal, cfg)
	if err != nil || result.Exported != 0 || result.AlreadyAcknowledged != 27 {
		t.Fatalf("repeat: %+v %v", result, err)
	}
	restarted, err := New(Options{Archive: s.archives[0].Journal.Dir})
	if err != nil {
		t.Fatal(err)
	}
	h2 := httptest.NewServer(restarted)
	defer h2.Close()
	status, b = request(t, h2, "GET", "/api/catalog", nil)
	var catalog struct {
		Archives []struct {
			Events []observepipe.Event           `json:"events"`
			Spans  []Span                        `json:"spans"`
			Trials []observepipe.TrialDatasetRow `json:"trials"`
			Error  string                        `json:"error"`
		} `json:"archives"`
	}
	if status != 200 || json.Unmarshal(b, &catalog) != nil || len(catalog.Archives) != 1 || catalog.Archives[0].Error != "" || len(catalog.Archives[0].Spans) != 27 || len(catalog.Archives[0].Trials) != 3 {
		t.Fatalf("bad restart catalog: %d", status)
	}
	status, b = request(t, h2, "GET", "/api/dataset.csv?archive="+s.archives[0].ID, nil)
	if status != 200 || !bytes.Contains(b, []byte("repomesh-dataset/1")) {
		t.Fatal("CSV unavailable")
	}
	status, b = request(t, h2, "GET", "/api/evidence?archive="+s.archives[0].ID+"&ref="+rows[0].EvidenceRefs[1], nil)
	if status != 200 || !json.Valid(b) {
		t.Fatal("evidence unavailable")
	}
	if err := os.WriteFile(filepath.Join(s.archives[0].Journal.Dir, rows[0].EvidenceRefs[1]), []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	_, b = request(t, h2, "GET", "/api/catalog", nil)
	if !bytes.Contains(b, []byte(`"error"`)) || bytes.Contains(b, []byte(`"trials"`)) {
		t.Fatal("corruption presented as valid trials")
	}
}

func TestReceiverJSONIdentityConflictAndWholeBatchValidation(t *testing.T) {
	s, h := newTestServer(t)
	span := map[string]any{"traceId": "11111111111111111111111111111111", "spanId": "2222222222222222", "name": "真实调用", "startTimeUnixNano": "1700000000000000000", "endTimeUnixNano": "1700000000010000000"}
	batch := func(spans ...any) map[string]any {
		return map[string]any{"resourceSpans": []any{map[string]any{"scopeSpans": []any{map[string]any{"spans": spans}}}}}
	}
	for range 2 {
		status, b := request(t, h, "POST", "/v1/traces", batch(span))
		if status != 200 {
			t.Fatalf("%d %s", status, b)
		}
	}
	spans, err := readSpans(s.archives[0].Journal)
	if err != nil || len(spans) != 1 || spans[0].DurationMS != 10 || spans[0].TraceID != span["traceId"] {
		t.Fatalf("%+v %v", spans, err)
	}
	span["name"] = "changed"
	newSpan := map[string]any{"traceId": span["traceId"], "spanId": "3333333333333333", "name": "new", "startTimeUnixNano": span["startTimeUnixNano"], "endTimeUnixNano": span["endTimeUnixNano"]}
	status, _ := request(t, h, "POST", "/v1/traces", batch(newSpan, span))
	if status != 409 {
		t.Fatal("conflict accepted")
	}
	spans, _ = readSpans(s.archives[0].Journal)
	if len(spans) != 1 {
		t.Fatal("partial conflicting batch persisted")
	}
	newSpan["spanId"] = "bad"
	status, _ = request(t, h, "POST", "/v1/traces", batch(newSpan))
	if status != 400 {
		t.Fatal("invalid identity accepted")
	}
}

func TestLoopbackAndBrowserBoundaries(t *testing.T) {
	s, h := newTestServer(t)
	for _, addr := range []string{"0.0.0.0:8090", "example.com:8090", ":8090"} {
		if ListenAddress(addr) == nil {
			t.Fatal("nonloopback allowed")
		}
	}
	for _, addr := range []string{"127.0.0.1:8090", "[::1]:8090"} {
		if ListenAddress(addr) != nil {
			t.Fatal("loopback rejected")
		}
	}
	for _, mode := range []string{"origin", "host", "header", "fetch-site"} {
		r, _ := http.NewRequest("POST", h.URL+"/api/trials", strings.NewReader(`{"variant":"suite"}`))
		r.Header.Set("X-RepoMesh-Local", "1")
		switch mode {
		case "origin":
			r.Header.Set("Origin", "https://untrusted.example")
		case "host":
			r.Host = "untrusted.example"
		case "header":
			r.Header.Del("X-RepoMesh-Local")
		case "fetch-site":
			r.Header.Set("Sec-Fetch-Site", "cross-site")
		}
		res, err := h.Client().Do(r)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != 403 {
			t.Fatalf("%s accepted", mode)
		}
	}
	status, _ := request(t, h, "GET", "/api/evidence?archive="+s.archives[0].ID+"&ref=../../model.json", nil)
	if status != 404 {
		t.Fatal("path traversal accepted")
	}
	status, _ = request(t, h, "POST", "/api/trials", map[string]string{"variant": "arbitrary-command"})
	if status != 400 {
		t.Fatal("arbitrary task accepted")
	}
	status, b := request(t, h, "GET", "/settings", nil)
	if status != 200 || !bytes.Contains(b, []byte("/app.js")) {
		t.Fatal("old settings route unavailable")
	}
	status, b = request(t, h, "GET", "/task-map.js", nil)
	if status != 200 || !bytes.Contains(b, []byte("renderTaskMap")) {
		t.Fatal("task map application unavailable")
	}
	for _, path := range []string{"/", "/settings", "/api/catalog"} {
		r, _ := http.NewRequest("GET", h.URL+path, nil)
		r.Header.Set("Sec-Fetch-Site", "cross-site")
		r.Header.Set("Sec-Fetch-Mode", "navigate")
		r.Header.Set("Sec-Fetch-Dest", "document")
		res, err := h.Client().Do(r)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		want := 200
		if path == "/api/catalog" {
			want = 403
		}
		if res.StatusCode != want {
			t.Fatalf("cross-site navigation to %s: %d", path, res.StatusCode)
		}
	}
}

func TestLegacyDeepSeekConfigurationRequiresExplicitJevMigration(t *testing.T) {
	s, h := newTestServer(t)
	if err := os.WriteFile(s.modelPath(), []byte(`{"model":"deepseek-flash","api_key":"legacy-test-key"}`), 0600); err != nil {
		t.Fatal(err)
	}
	status, body := request(t, h, "GET", "/api/settings", nil)
	if status != 200 || bytes.Contains(body, []byte("legacy-test-key")) {
		t.Fatal("legacy metadata unavailable or key leaked")
	}
	status, _ = request(t, h, "POST", "/api/judge", map[string]string{"archive": s.archives[0].ID, "trial_id": "any"})
	if status != 409 {
		t.Fatal("legacy generator still used as rubric scorer")
	}
	status, _ = request(t, h, "PUT", "/api/model", map[string]string{"provider": "typesafe", "model": "jev-1.13.0", "api_key": ""})
	if status != 400 {
		t.Fatal("legacy key reused across providers")
	}
	status, _ = request(t, h, "PUT", "/api/model", map[string]string{"provider": "typesafe", "model": "jev-1.13.0", "api_key": "new-test-key"})
	if status != 200 {
		t.Fatal("explicit Jev migration failed")
	}
	info, _ := os.Stat(s.modelPath())
	if info.Mode().Perm() != 0600 {
		t.Fatal("key file not private")
	}
}
