package observepipe

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	collector "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/protobuf/proto"
)

func TestOTLPProtocolStableIdentityAllowlistAndFailureReceipts(t *testing.T) {
	j, _ := OpenJournal(filepath.Join(t.TempDir(), "archive"))
	e := journalEvent(t, j, "one")
	saved, _, err := j.Append(e)
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int64
	mode := atomic.Int32{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/v1/traces" || r.Header.Get("Content-Type") != "application/x-protobuf" {
			t.Error("incorrect OTLP request")
		}
		if r.Header.Get("Authorization") != "Bearer local-secret" {
			t.Error("missing configured authorization")
		}
		b, _ := io.ReadAll(r.Body)
		if bytes.Contains(b, []byte("private-input")) || bytes.Contains(b, []byte("local-secret")) {
			t.Error("payload leaked raw data or credentials")
		}
		var request collector.ExportTraceServiceRequest
		if err := proto.Unmarshal(b, &request); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		span := request.ResourceSpans[0].ScopeSpans[0].Spans[0]
		if len(span.TraceId) != 16 || len(span.SpanId) != 8 || span.StartTimeUnixNano != span.EndTimeUnixNano {
			t.Error("invalid persisted point-event trace")
		}
		if mode.Load() == 1 {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte("SECRET response should not be logged"))
			return
		}
		response := &collector.ExportTraceServiceResponse{}
		if mode.Load() == 2 {
			response.PartialSuccess = &collector.ExportTracePartialSuccess{RejectedSpans: 1, ErrorMessage: "SECRET partial"}
		}
		w.Header().Set("Content-Type", "application/x-protobuf")
		raw, _ := proto.Marshal(response)
		_, _ = w.Write(raw)
	}))
	defer server.Close()
	t.Setenv("TEST_OTLP_ENDPOINT", server.URL+"/v1/traces")
	t.Setenv("TEST_OTLP_HEADERS", "Authorization=Bearer%20local-secret")
	c := ExportConfig{DestinationID: "test", EndpointEnv: "TEST_OTLP_ENDPOINT", HeadersEnv: "TEST_OTLP_HEADERS", ServiceName: "test", TimeoutSeconds: 1}
	mode.Store(1)
	summary, err := ExportOTLP(context.Background(), j, c)
	if err == nil || summary.Exported != 0 || strings.Contains(err.Error(), "SECRET") || strings.Contains(err.Error(), server.URL) {
		t.Fatal("unsafe or false success", summary, err)
	}
	mode.Store(2)
	summary, err = ExportOTLP(context.Background(), j, c)
	if err == nil || summary.Exported != 0 {
		t.Fatal("partial rejection marked success", summary, err)
	}
	mode.Store(0)
	summary, err = ExportOTLP(context.Background(), j, c)
	if err != nil || summary.Exported != 1 {
		t.Fatal(summary, err)
	}
	after := calls.Load()
	summary, err = ExportOTLP(context.Background(), j, c)
	if err != nil || summary.AlreadyAcknowledged != 1 || calls.Load() != after {
		t.Fatal("duplicate export", summary, err)
	}
	read, err := j.ReadEvent(saved.EventID)
	if err != nil || read.TraceID != saved.TraceID || read.SpanID != saved.SpanID {
		t.Fatal("identity changed on retry")
	}
	files, _ := filepath.Glob(filepath.Join(j.Dir, "receipts", "*.json"))
	for _, f := range files {
		b, _ := os.ReadFile(f)
		if bytes.Contains(b, []byte("local-secret")) || bytes.Contains(b, []byte(server.URL)) {
			t.Fatal("credentials leaked in receipt")
		}
	}
}

func TestOTLPRejectsInsecureRemoteAndRedirect(t *testing.T) {
	j, _ := OpenJournal(filepath.Join(t.TempDir(), "archive"))
	e := journalEvent(t, j, "one")
	_, _, _ = j.Append(e)
	t.Setenv("TEST_ENDPOINT", "http://192.0.2.1/v1/traces")
	c := ExportConfig{DestinationID: "test", EndpointEnv: "TEST_ENDPOINT", ServiceName: "test", TimeoutSeconds: 1}
	if _, err := ExportOTLP(t.Context(), j, c); err == nil {
		t.Fatal("remote plaintext allowed")
	}
	var redirected atomic.Int64
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { redirected.Add(1) }))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer source.Close()
	t.Setenv("TEST_ENDPOINT", source.URL+"/v1/traces")
	if _, err := ExportOTLP(t.Context(), j, c); err == nil || redirected.Load() != 0 {
		t.Fatal("redirect followed or acknowledged")
	}
}
