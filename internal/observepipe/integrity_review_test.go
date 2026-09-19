package observepipe

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
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

func TestOTLPRejectsNonProtocolAcknowledgmentAndRetriesSameSpan(t *testing.T) {
	for _, tc := range []struct {
		name, contentType, body string
		status                  int
	}{
		{"html", "text/html", "<html>private-response-marker</html>", http.StatusOK},
		{"missing_type", "", "", http.StatusOK},
		{"no_content", "application/x-protobuf", "", http.StatusNoContent},
		{"malformed_protobuf", "application/x-protobuf", "private-response-marker", http.StatusOK},
		{"malformed_json", "application/json", "private-response-marker", http.StatusOK},
		{"partial_json_charset", "application/json; charset=utf-8", `{"partialSuccess":{"rejectedSpans":"1","errorMessage":"private-response-marker"}}`, http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			j, err := OpenJournal(filepath.Join(t.TempDir(), "archive"))
			if err != nil {
				t.Fatal(err)
			}
			e, _, err := j.Append(journalEvent(t, j, "review-response"))
			if err != nil {
				t.Fatal(err)
			}
			expectedTrace, _ := hex.DecodeString(e.TraceID)
			expectedSpan, _ := hex.DecodeString(e.SpanID)
			var valid atomic.Bool
			var calls atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				body, err := io.ReadAll(r.Body)
				var request collector.ExportTraceServiceRequest
				if err != nil || proto.Unmarshal(body, &request) != nil || len(request.ResourceSpans) != 1 || len(request.ResourceSpans[0].ScopeSpans) != 1 || len(request.ResourceSpans[0].ScopeSpans[0].Spans) != 1 {
					t.Error("request is not one OTLP span")
					w.WriteHeader(400)
					return
				}
				span := request.ResourceSpans[0].ScopeSpans[0].Spans[0]
				if !bytes.Equal(span.TraceId, expectedTrace) || !bytes.Equal(span.SpanId, expectedSpan) {
					t.Error("retry changed the persisted trace or span identity")
				}
				if valid.Load() {
					w.Header().Set("Content-Type", "application/x-protobuf")
					w.WriteHeader(http.StatusOK)
					return
				}
				if tc.contentType != "" {
					w.Header().Set("Content-Type", tc.contentType)
				}
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer server.Close()
			t.Setenv("REVIEW_OTLP_ENDPOINT", server.URL+"/v1/traces")
			c := ExportConfig{DestinationID: "review-response", EndpointEnv: "REVIEW_OTLP_ENDPOINT", ServiceName: "review", TimeoutSeconds: 2}
			summary, err := ExportOTLP(t.Context(), j, c)
			if err == nil || summary.Exported != 0 || summary.AlreadyAcknowledged != 0 {
				t.Fatalf("invalid response acknowledged: %+v, error=%v", summary, err)
			}
			if strings.Contains(err.Error(), "private-response-marker") || strings.Contains(err.Error(), server.URL) {
				t.Fatal("untrusted response or destination leaked through export error")
			}
			valid.Store(true)
			summary, err = ExportOTLP(t.Context(), j, c)
			if err != nil || summary.Exported != 1 || calls.Load() != 2 {
				t.Fatalf("rejected span was not retried: %+v, calls=%d, error=%v", summary, calls.Load(), err)
			}
			summary, err = ExportOTLP(t.Context(), j, c)
			if err != nil || summary.AlreadyAcknowledged != 1 || calls.Load() != 2 {
				t.Fatalf("confirmed span resent: %+v, calls=%d, error=%v", summary, calls.Load(), err)
			}
		})
	}
}

func TestOTLPRequiresInputEvidenceEvenAfterAcknowledgment(t *testing.T) {
	for _, acknowledged := range []bool{false, true} {
		name := "before_first_send"
		if acknowledged {
			name = "after_acknowledgment"
		}
		t.Run(name, func(t *testing.T) {
			j, err := OpenJournal(filepath.Join(t.TempDir(), "archive"))
			if err != nil {
				t.Fatal(err)
			}
			e := journalEvent(t, j, "review-input")
			input, err := j.PutJSON(map[string]string{"source": "separate immutable input"})
			if err != nil {
				t.Fatal(err)
			}
			e.InputArtifactRefs = []string{input}
			if _, _, err = j.Append(e); err != nil {
				t.Fatal(err)
			}
			var calls atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				_, _ = io.Copy(io.Discard, r.Body)
				w.Header().Set("Content-Type", "application/x-protobuf")
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()
			t.Setenv("REVIEW_OTLP_ENDPOINT", server.URL+"/v1/traces")
			c := ExportConfig{DestinationID: "review-evidence", EndpointEnv: "REVIEW_OTLP_ENDPOINT", ServiceName: "review", TimeoutSeconds: 2}
			if acknowledged {
				if _, err = ExportOTLP(t.Context(), j, c); err != nil {
					t.Fatal(err)
				}
			}
			before := calls.Load()
			if err = os.Remove(filepath.Join(j.Dir, input)); err != nil {
				t.Fatal(err)
			}
			summary, err := ExportOTLP(t.Context(), j, c)
			if err == nil || summary.Exported != 0 || summary.AlreadyAcknowledged != 0 || calls.Load() != before {
				t.Fatalf("missing input evidence was ignored: %+v, calls before=%d after=%d, error=%v", summary, before, calls.Load(), err)
			}
		})
	}
}

func TestDatasetRequiresManifestAndExactEventTraceAssociations(t *testing.T) {
	r, err := RunDiscountFixture(t.Context(), "candidate")
	if err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []string{"missing_manifest", "missing_report", "missing_event", "unrelated_trace", "unrelated_manifest", "unrelated_event"} {
		t.Run(mutate, func(t *testing.T) {
			j, err := OpenJournal(filepath.Join(t.TempDir(), "archive"))
			if err != nil {
				t.Fatal(err)
			}
			a, err := ArchiveTrial(j, r)
			if err != nil {
				t.Fatal(err)
			}
			if rows, err := DatasetRows(j); err != nil || len(rows) != 1 {
				t.Fatalf("valid trial unavailable: rows=%d, error=%v", len(rows), err)
			}
			switch mutate {
			case "missing_manifest":
				err = os.Remove(filepath.Join(j.Dir, a.ManifestRef))
			case "missing_report":
				err = os.Remove(filepath.Join(j.Dir, a.ReportRef))
			case "missing_event":
				err = os.Remove(filepath.Join(j.Dir, "events", a.EventIDs[0]+".json"))
			case "unrelated_trace":
				a.TraceIDs[0] = strings.Repeat("1", 32)
			case "unrelated_manifest":
				a.ManifestRef, err = j.PutJSON(map[string]string{"schema_version": "different-manifest"})
			case "unrelated_event":
				other := r
				other.TrialID += "-other"
				b, archiveErr := ArchiveTrial(j, other)
				err = archiveErr
				if err == nil {
					a.EventIDs[0], a.TraceIDs[0] = b.EventIDs[0], b.TraceIDs[0]
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			if strings.HasPrefix(mutate, "unrelated_") {
				// Model a stale or incorrectly associated producer record with a
				// valid checksum, so integrity must include semantic identity.
				data, err := json.Marshal(a)
				if err != nil {
					t.Fatal(err)
				}
				wrapped, err := json.Marshal(diskRecord{Checksum: Digest(data), Data: data})
				if err != nil {
					t.Fatal(err)
				}
				if err = os.WriteFile(filepath.Join(j.Dir, "trials", Digest([]byte(a.TrialID))+".json"), wrapped, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if rows, err := DatasetRows(j); err == nil || len(rows) != 0 {
				t.Fatalf("invalid association exported: rows=%d, error=%v", len(rows), err)
			}
		})
	}
}
