// Package observeui hosts the loopback-only observation and evaluation workbench.
// It is an independent management tool, never a product process dependency.
package observeui

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"repomesh.local/repomesh/internal/observepipe"
)

//go:embed static/*
var assets embed.FS

type Options struct {
	Archive      string
	ReadArchives []string
}
type archive struct {
	ID       string
	Journal  *observepipe.Journal
	Writable bool
}
type Server struct {
	archives      []archive
	mu            sync.Mutex
	running       sync.Mutex
	modelMu       sync.Mutex
	client        *http.Client
	modelEndpoint string
}

func New(options Options) (*Server, error) {
	j, err := observepipe.OpenJournal(options.Archive)
	if err != nil {
		return nil, err
	}
	s := &Server{client: &http.Client{Timeout: 90 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return errors.New("model redirects are disabled") }}, modelEndpoint: "https://api.deepseek.com"}
	paths := append([]string{j.Dir}, options.ReadArchives...)
	seen := map[string]bool{}
	for i, path := range paths {
		abs, err := filepath.Abs(path)
		if err != nil {
			return nil, err
		}
		if seen[abs] {
			continue
		}
		seen[abs] = true
		if i > 0 {
			if _, err := os.Stat(abs); err != nil {
				return nil, errors.New("read archive does not exist")
			}
		}
		journal := j
		if i > 0 {
			journal, err = observepipe.OpenJournalReadOnly(abs)
		}
		if err != nil {
			return nil, err
		}
		s.archives = append(s.archives, archive{ID: observepipe.Digest([]byte(abs))[:16], Journal: journal, Writable: i == 0})
	}
	return s, nil
}

func ListenAddress(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return errors.New("provide an explicit loopback IP and port")
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return errors.New("workbench must listen on a loopback IP")
	}
	return nil
}

func Serve(ctx context.Context, addr string, s *Server, out io.Writer) error {
	if err := ListenAddress(addr); err != nil {
		return err
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	server := &http.Server{Handler: s, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 20 * time.Second, WriteTimeout: 120 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 32 << 10}
	fmt.Fprintf(out, "RepoMesh local workbench: http://%s\n", listener.Addr())
	go s.runOnline(ctx)
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = server.Shutdown(shutdown)
		case <-done:
		}
	}()
	err = server.Serve(listener)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func loopbackHost(hostport string) bool {
	host := hostport
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		host = h
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
	// A link from the HTTPS product console may navigate to this HTTP loopback
	// page. Permit only the document entry; cross-site API reads/writes remain
	// forbidden. The page then makes its own same-origin requests.
	pageNavigation := r.Method == "GET" && (r.URL.Path == "/" || r.URL.Path == "/settings") &&
		r.Header.Get("Sec-Fetch-Mode") == "navigate" && r.Header.Get("Sec-Fetch-Dest") == "document"
	if !loopbackHost(r.Host) || (r.Header.Get("Sec-Fetch-Site") == "cross-site" && !pageNavigation) {
		failHTTP(w, 403, "only local access is allowed")
		return
	}
	if origin := r.Header.Get("Origin"); origin != "" {
		u, err := url.Parse(origin)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || !loopbackHost(u.Host) {
			failHTTP(w, 403, "foreign origin is not allowed")
			return
		}
	}
	if r.Method != "GET" && r.Method != "HEAD" && r.URL.Path != "/v1/traces" && r.Header.Get("X-RepoMesh-Local") != "1" {
		failHTTP(w, 403, "local request header required")
		return
	}
	switch {
	case r.Method == "GET" && r.URL.Path == "/api/health":
		writeJSON(w, 200, map[string]any{"status": "ok", "service": "repomesh-local-observe", "cloud_required": false})
	case r.Method == "GET" && r.URL.Path == "/api/assistant":
		s.assistantSettings(w, r)
	case r.Method == "PUT" && r.URL.Path == "/api/assistant":
		s.saveAssistant(w, r)
	case r.Method == "POST" && r.URL.Path == "/api/assistant/test":
		s.testAssistant(w, r)
	case r.Method == "GET" && r.URL.Path == "/api/analyses":
		s.analysisList(w, r)
	case r.Method == "POST" && r.URL.Path == "/api/analyses/attribution":
		s.analyzeSample(w, r)
	case r.Method == "POST" && r.URL.Path == "/api/analyses/clustering":
		s.clusterSamples(w, r)
	case r.Method == "POST" && r.URL.Path == "/api/annotations":
		s.annotateLocal(w, r)
	case (r.Method == "GET" || r.Method == "PUT") && r.URL.Path == "/api/evaluation-policy":
		s.evaluationPolicy(w, r)
	case r.Method == "GET" && r.URL.Path == "/api/status":
		s.connectionStatus(w, r)
	case r.Method == "PUT" && r.URL.Path == "/api/integration":
		s.saveIntegration(w, r)
	case r.Method == "GET" && r.URL.Path == "/api/evaluations":
		s.evaluationList(w, r)
	case r.Method == "POST" && r.URL.Path == "/api/platform/import":
		s.importPlatformRun(w, r)
	case r.Method == "GET" && r.URL.Path == "/api/samples":
		s.listSamples(w, r)
	case r.Method == "POST" && r.URL.Path == "/api/samples":
		s.createSample(w, r)
	case r.Method == "GET" && r.URL.Path == "/api/samples/export":
		s.exportSample(w, r)
	case r.Method == "POST" && r.URL.Path == "/api/annotations/import":
		s.importAnnotation(w, r)
	case r.Method == "GET" && r.URL.Path == "/api/trace":
		s.traceDetails(w, r)
	case r.Method == "GET" && r.URL.Path == "/api/calls":
		s.queryCalls(w, r)
	case r.Method == "GET" && r.URL.Path == "/api/metrics":
		s.metrics(w, r)
	case r.Method == "POST" && r.URL.Path == "/api/bindings":
		s.registerBinding(w, r)
	case r.Method == "GET" && r.URL.Path == "/api/rubric":
		writeJSON(w, 200, observepipe.ObservationRubric())
	case r.Method == "GET" && r.URL.Path == "/api/catalog":
		s.catalog(w, r)
	case r.Method == "GET" && r.URL.Path == "/api/evidence":
		s.evidence(w, r)
	case r.Method == "GET" && r.URL.Path == "/api/dataset.csv":
		s.dataset(w, r)
	case r.Method == "POST" && r.URL.Path == "/api/trials":
		s.runTrial(w, r)
	case r.Method == "POST" && r.URL.Path == "/v1/traces":
		s.ingest(w, r)
	case r.Method == "GET" && r.URL.Path == "/api/settings":
		s.settings(w, r)
	case r.Method == "PUT" && r.URL.Path == "/api/model":
		s.saveModel(w, r)
	case r.Method == "POST" && r.URL.Path == "/api/model/test":
		s.testModel(w, r)
	case r.Method == "POST" && r.URL.Path == "/api/judge":
		s.judge(w, r)
	case (r.Method == "GET" || r.Method == "HEAD") && (r.URL.Path == "/" || r.URL.Path == "/settings" || r.URL.Path == "/app.js" || r.URL.Path == "/extended.js" || r.URL.Path == "/style.css"):
		name := r.URL.Path
		if name == "/" || name == "/settings" {
			name = "/index.html"
		}
		b, err := assets.ReadFile("static" + name)
		if err != nil {
			failHTTP(w, 500, "page unavailable")
			return
		}
		w.Header().Set("Content-Type", mime.TypeByExtension(filepath.Ext(name)))
		if r.Method != "HEAD" {
			_, _ = w.Write(b)
		}
	default:
		failHTTP(w, 404, "not found")
	}
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func failHTTP(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
func readJSON(w http.ResponseWriter, r *http.Request, value any) bool {
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	d.DisallowUnknownFields()
	if d.Decode(value) != nil || d.Decode(new(any)) != io.EOF {
		failHTTP(w, 400, "invalid JSON request")
		return false
	}
	return true
}
func (s *Server) findArchive(id string) *archive {
	for i := range s.archives {
		if s.archives[i].ID == id {
			return &s.archives[i]
		}
	}
	return nil
}

func (s *Server) catalog(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	items := []map[string]any{}
	for _, a := range s.archives {
		item := map[string]any{"id": a.ID, "name": filepath.Base(a.Journal.Dir), "writable": a.Writable}
		events, err := a.Journal.Events()
		if err == nil {
			for _, e := range events {
				for _, ref := range append(append([]string{e.ContextManifestRef}, e.EvidenceRefs...), e.InputArtifactRefs...) {
					var v json.RawMessage
					if err = a.Journal.ReadEvidence(ref, &v); err != nil {
						break
					}
				}
				if err != nil {
					break
				}
			}
		}
		if err != nil {
			item["error"] = "证据档案校验失败"
			items = append(items, item)
			continue
		}
		rows, err := observepipe.DatasetRows(a.Journal)
		if err != nil {
			item["error"] = "验收档案校验失败"
			items = append(items, item)
			continue
		}
		spans, err := readSpans(a.Journal)
		if err != nil {
			item["error"] = "Trace 档案校验失败"
			items = append(items, item)
			continue
		}
		judgments, err := readJudgments(a.Journal)
		if err != nil {
			item["error"] = "AI 评审档案校验失败"
			items = append(items, item)
			continue
		}
		item["events"] = events
		item["trials"] = rows
		item["spans"] = spans
		item["judgments"] = judgments
		items = append(items, item)
	}
	writeJSON(w, 200, map[string]any{"archives": items, "updated_at": time.Now().UTC(), "dsh": "not_connected"})
}

func (s *Server) evidence(w http.ResponseWriter, r *http.Request) {
	a := s.findArchive(r.URL.Query().Get("archive"))
	if a == nil {
		failHTTP(w, 404, "archive not found")
		return
	}
	var body json.RawMessage
	if err := a.Journal.ReadEvidence(r.URL.Query().Get("ref"), &body); err != nil {
		failHTTP(w, 404, "evidence missing or checksum mismatch")
		return
	}
	writeJSON(w, 200, body)
}

func (s *Server) dataset(w http.ResponseWriter, r *http.Request) {
	a := s.findArchive(r.URL.Query().Get("archive"))
	if a == nil {
		failHTTP(w, 404, "archive not found")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := observepipe.DatasetRows(a.Journal)
	if err != nil {
		failHTTP(w, 409, "dataset integrity check failed")
		return
	}
	var b bytes.Buffer
	if err = observepipe.WriteTrialCSV(&b, rows); err != nil {
		failHTTP(w, 409, "dataset export failed")
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="repomesh-trials.csv"`)
	_, _ = w.Write(b.Bytes())
}

func (s *Server) runTrial(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		Variant string `json:"variant"`
	}
	if !readJSON(w, r, &payload) {
		return
	}
	variants := []string{payload.Variant}
	if payload.Variant == "suite" {
		variants = []string{"baseline", "candidate", "assembly-mismatch"}
	}
	for _, v := range variants {
		if v != "baseline" && v != "candidate" && v != "assembly-mismatch" {
			failHTTP(w, 400, "unknown fixed case variant")
			return
		}
	}
	if !s.running.TryLock() {
		failHTTP(w, 409, "an evaluation is already running")
		return
	}
	defer s.running.Unlock()
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	results := []observepipe.ArchivedTrial{}
	for _, v := range variants {
		result, err := observepipe.RunDiscountFixture(ctx, v)
		if err != nil {
			failHTTP(w, 500, "local verification could not finish")
			return
		}
		s.mu.Lock()
		a, err := observepipe.ArchiveTrial(s.archives[0].Journal, result)
		s.mu.Unlock()
		if err != nil {
			failHTTP(w, 500, "cannot persist verification result")
			return
		}
		results = append(results, a)
	}
	writeJSON(w, 201, map[string]any{"trials": results, "archive": s.archives[0].ID})
}

func (s *Server) ingest(w http.ResponseWriter, r *http.Request) {
	content, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || (content != "application/json" && content != "application/x-protobuf") {
		failHTTP(w, 415, "OTLP JSON or protobuf required")
		return
	}
	if r.Header.Get("Content-Encoding") != "" {
		failHTTP(w, 415, "send uncompressed OTLP")
		return
	}
	b, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 16<<20))
	if err != nil {
		failHTTP(w, 413, "OTLP request exceeds limit")
		return
	}
	spans, err := decodeOTLP(b, content == "application/json")
	if err != nil {
		failHTTP(w, 400, "invalid OTLP span payload")
		return
	}
	s.mu.Lock()
	err = storeSpans(s.archives[0].Journal, spans)
	s.mu.Unlock()
	if err != nil {
		failHTTP(w, 409, "span conflict or storage failure; batch not acknowledged")
		return
	}
	if content == "application/json" {
		writeJSON(w, 200, map[string]any{})
		return
	}
	w.Header().Set("Content-Type", "application/x-protobuf")
	w.WriteHeader(200)
}
