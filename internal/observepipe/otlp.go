package observepipe

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

const exportMappingVersion = "repomesh-otlp/0.1"

// ExportConfig holds references to secrets, never secrets themselves. Endpoint
// is a complete traces URL, not a base URL to which arbitrary paths are added.
type ExportConfig struct {
	DestinationID  string `json:"destination_id"`
	EndpointEnv    string `json:"endpoint_env"`
	HeadersEnv     string `json:"headers_env"`
	ServiceName    string `json:"service_name"`
	Workspace      string `json:"workspace,omitempty"`
	TimeoutSeconds int    `json:"timeout_seconds"`
}

func LoadExportConfig(path string) (ExportConfig, error) {
	f, err := os.Open(path)
	if err != nil {
		return ExportConfig{}, errors.New("cannot read exporter configuration")
	}
	defer f.Close()
	d := json.NewDecoder(io.LimitReader(f, 65537))
	d.DisallowUnknownFields()
	var c ExportConfig
	if d.Decode(&c) != nil {
		return c, errors.New("invalid exporter configuration")
	}
	if d.Decode(new(any)) != io.EOF {
		return c, errors.New("unexpected exporter configuration content")
	}
	if strings.TrimSpace(c.DestinationID) == "" || strings.TrimSpace(c.EndpointEnv) == "" || strings.TrimSpace(c.ServiceName) == "" {
		return c, errors.New("destination_id, endpoint_env and service_name are required")
	}
	if c.TimeoutSeconds < 1 || c.TimeoutSeconds > 60 {
		return c, errors.New("timeout_seconds must be between 1 and 60")
	}
	return c, nil
}

func (c ExportConfig) resolve() (string, map[string]string, string, error) {
	endpoint := strings.TrimSpace(os.Getenv(c.EndpointEnv))
	u, err := url.Parse(endpoint)
	if err != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path == "" || !oneOf(u.Scheme, "http", "https") {
		return "", nil, "", errors.New("configure an explicit OTLP traces URL in the endpoint environment variable")
	}
	ip := net.ParseIP(u.Hostname())
	if u.Hostname() != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return "", nil, "", errors.New("observation is local-only; configure a loopback OTLP collector")
	}
	if u.Scheme == "http" {
		ip := net.ParseIP(u.Hostname())
		if u.Hostname() != "localhost" && (ip == nil || !ip.IsLoopback()) {
			return "", nil, "", errors.New("plain HTTP is allowed only for a loopback collector")
		}
	}
	headers := map[string]string{}
	if c.HeadersEnv != "" {
		for _, item := range strings.Split(os.Getenv(c.HeadersEnv), ",") {
			if strings.TrimSpace(item) == "" {
				continue
			}
			k, v, ok := strings.Cut(item, "=")
			k = strings.TrimSpace(k)
			decoded, decodeErr := url.PathUnescape(strings.TrimSpace(v))
			if !ok || k == "" || decodeErr != nil || strings.ContainsAny(k+decoded, "\r\n") {
				return "", nil, "", errors.New("invalid OTLP authentication headers")
			}
			if strings.EqualFold(k, "Host") || strings.EqualFold(k, "Content-Type") {
				return "", nil, "", errors.New("reserved OTLP header override")
			}
			headers[k] = decoded
		}
	}
	// Only this digest is stored: endpoint paths and header values may contain
	// credentials. A changed destination/auth scope cannot reuse old receipts.
	b, _ := json.Marshal([]any{exportMappingVersion, c.DestinationID, endpoint, headers, c.ServiceName, c.Workspace})
	return endpoint, headers, Digest(b), nil
}

type ExportSummary struct {
	Exported            int    `json:"exported"`
	AlreadyAcknowledged int    `json:"already_acknowledged"`
	DestinationID       string `json:"destination_id"`
	Status              string `json:"status"`
}
type exportReceipt struct {
	EventID           string `json:"event_id"`
	EventFingerprint  string `json:"event_fingerprint"`
	DestinationDigest string `json:"destination_digest"`
	MappingVersion    string `json:"mapping_version"`
	TraceID           string `json:"trace_id"`
	SpanID            string `json:"span_id"`
	Status            string `json:"status"`
}

// ExportOTLP sends persisted projections with stable IDs. A transport receipt is
// not proof that AgentLoop indexed the data or ran an evaluator.
func ExportOTLP(ctx context.Context, j *Journal, c ExportConfig) (ExportSummary, error) {
	out := ExportSummary{DestinationID: c.DestinationID, Status: "pending"}
	endpoint, headers, destination, err := c.resolve()
	if err != nil {
		return out, err
	}
	events, err := j.Events()
	if err != nil {
		return out, err
	}
	byID := map[string]Event{}
	for _, e := range events {
		byID[e.EventID] = e
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: strictOTLPResponse{transport}, Timeout: time.Duration(c.TimeoutSeconds) * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	options := []otlptracehttp.Option{otlptracehttp.WithEndpointURL(endpoint), otlptracehttp.WithEncoding(otlptracehttp.EncodingProtobuf), otlptracehttp.WithHeaders(headers), otlptracehttp.WithHTTPClient(client), otlptracehttp.WithRetry(otlptracehttp.RetryConfig{Enabled: false})}
	exporter, err := otlptracehttp.New(ctx, options...)
	if err != nil {
		return out, errors.New("cannot initialize OTLP exporter")
	}
	defer exporter.Shutdown(context.Background())
	for _, e := range events {
		// Evidence availability is checked even for acknowledged events. An old
		// transport receipt cannot make a damaged local archive complete again.
		for _, ref := range append(append([]string{e.ContextManifestRef}, e.EvidenceRefs...), e.InputArtifactRefs...) {
			var raw json.RawMessage
			if err = j.ReadEvidence(ref, &raw); err != nil {
				return out, err
			}
		}
		receipt := exportReceipt{EventID: e.EventID, EventFingerprint: e.semanticFingerprint(), DestinationDigest: destination, MappingVersion: exportMappingVersion, TraceID: e.TraceID, SpanID: e.SpanID, Status: "transport_acknowledged"}
		key := destination + ":" + e.EventID
		var old exportReceipt
		if err = j.ReadRecord("receipts", key, &old); err == nil {
			if old != receipt {
				return out, errors.New("export receipt does not match archived event")
			}
			out.AlreadyAcknowledged++
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			return out, err
		}
		span, err := projectSpan(e, byID, c)
		if err != nil {
			return out, err
		}
		if err = exporter.ExportSpans(ctx, []sdktrace.ReadOnlySpan{span}); err != nil {
			out.Status = "failed"
			// Do not expose exporter errors: they can contain the auth-bearing URL
			// or a response body from the remote endpoint.
			_ = j.PutRecord("receipts", "failed:"+key+":"+time.Now().UTC().Format(time.RFC3339Nano), map[string]any{"event_id": e.EventID, "destination_digest": destination, "status": "not_fully_acknowledged", "at": time.Now().UTC()})
			return out, errors.New("OTLP export was not fully acknowledged; local evidence retained")
		}
		if err = j.PutRecord("receipts", key, receipt); err != nil {
			return out, err
		}
		out.Exported++
	}
	out.Status = "transport_acknowledged"
	return out, nil
}

// The upstream SDK accepts any 2xx/unknown content type. Require the OTLP
// response contract here so a console HTML page cannot acknowledge a trace.
type strictOTLPResponse struct{ base http.RoundTripper }

func (t strictOTLPResponse) RoundTrip(r *http.Request) (*http.Response, error) {
	resp, err := t.base.RoundTrip(r)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 200 && resp.StatusCode <= 299 {
		media, _, parseErr := mime.ParseMediaType(resp.Header.Get("Content-Type"))
		if resp.StatusCode != http.StatusOK || parseErr != nil || !oneOf(media, "application/x-protobuf", "application/json") {
			resp.Body.Close()
			return nil, errors.New("invalid OTLP response status or content type")
		}
		resp.Header.Set("Content-Type", media)
	}
	return resp, nil
}

type fixedIDs struct {
	tid trace.TraceID
	sid trace.SpanID
}

func (f fixedIDs) NewIDs(context.Context) (trace.TraceID, trace.SpanID)  { return f.tid, f.sid }
func (f fixedIDs) NewSpanID(context.Context, trace.TraceID) trace.SpanID { return f.sid }

type captureProcessor struct {
	mu   sync.Mutex
	span sdktrace.ReadOnlySpan
}

func (*captureProcessor) OnStart(context.Context, sdktrace.ReadWriteSpan) {}
func (p *captureProcessor) OnEnd(s sdktrace.ReadOnlySpan) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.span = s
}
func (*captureProcessor) Shutdown(context.Context) error   { return nil }
func (*captureProcessor) ForceFlush(context.Context) error { return nil }

func projectSpan(e Event, byID map[string]Event, c ExportConfig) (sdktrace.ReadOnlySpan, error) {
	if err := e.Validate(); err != nil {
		return nil, err
	}
	tid, _ := trace.TraceIDFromHex(e.TraceID)
	sid, _ := trace.SpanIDFromHex(e.SpanID)
	attrs := []attribute.KeyValue{attribute.String("repomesh.schema_version", e.SchemaVersion), attribute.String("repomesh.event_id", e.EventID), attribute.String("repomesh.event_name", e.EventName), attribute.String("repomesh.work_id", e.WorkID), attribute.String("repomesh.context_manifest_ref", e.ContextManifestRef), attribute.String("repomesh.collection_status", e.CollectionStatus), attribute.String("repomesh.outcome_verdict", e.OutcomeVerdict), attribute.String("repomesh.execution_status", e.ExecutionStatus), attribute.String("repomesh.evidence_level", e.EvidenceLevel), attribute.String("repomesh.duration_kind", "instantaneous_fact"), attribute.String("repomesh.mapping_version", exportMappingVersion), attribute.StringSlice("repomesh.evidence_refs", e.EvidenceRefs), attribute.StringSlice("repomesh.missing_fields", e.MissingFields), attribute.StringSlice("repomesh.caused_by_event_ids", e.CausedByEventIDs)}
	for k, v := range map[string]string{"repomesh.project_id": e.ProjectID, "repomesh.issue_id": e.IssueID, "repomesh.check_id": e.CheckID, "eval.trial_id": e.TrialID, "eval.case_id": e.CaseID, "eval.variant_id": e.VariantID} {
		if v != "" {
			attrs = append(attrs, attribute.String(k, v))
		}
	}
	links := []trace.Link{}
	for _, id := range e.CausedByEventIDs {
		parent, ok := byID[id]
		if !ok {
			return nil, errors.New("event causal reference is missing")
		}
		t, _ := trace.TraceIDFromHex(parent.TraceID)
		s, _ := trace.SpanIDFromHex(parent.SpanID)
		links = append(links, trace.Link{SpanContext: trace.NewSpanContext(trace.SpanContextConfig{TraceID: t, SpanID: s, TraceFlags: trace.FlagsSampled})})
	}
	resources := []attribute.KeyValue{attribute.String("service.name", c.ServiceName), attribute.String("service.version", exportMappingVersion), attribute.String("acs.arms.service.feature", "genai_app"), attribute.String("repomesh.source.version", e.ProducerVersion)}
	if c.Workspace != "" {
		resources = append(resources, attribute.String("acs.cms.workspace", c.Workspace))
	}
	capture := &captureProcessor{}
	p := sdktrace.NewTracerProvider(sdktrace.WithIDGenerator(fixedIDs{tid, sid}), sdktrace.WithSampler(sdktrace.AlwaysSample()), sdktrace.WithResource(resource.NewSchemaless(resources...)), sdktrace.WithSpanProcessor(capture))
	_, span := p.Tracer("repomesh-observe").Start(context.Background(), e.EventName, trace.WithTimestamp(e.OccurredAt), trace.WithAttributes(attrs...), trace.WithLinks(links...))
	// These are committed point facts. Equal start/end is intentional; polling
	// and export time must never masquerade as real business execution duration.
	span.End(trace.WithTimestamp(e.OccurredAt))
	_ = p.Shutdown(context.Background())
	return capture.span, nil
}
