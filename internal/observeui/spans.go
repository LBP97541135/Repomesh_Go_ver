package observeui

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"time"

	collector "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"repomesh.local/repomesh/internal/observepipe"
)

type Span struct {
	TraceID      string          `json:"trace_id"`
	SpanID       string          `json:"span_id"`
	ParentSpanID string          `json:"parent_span_id,omitempty"`
	Name         string          `json:"name"`
	Service      string          `json:"service"`
	Start        time.Time       `json:"start"`
	End          time.Time       `json:"end"`
	DurationMS   float64         `json:"duration_ms"`
	Status       string          `json:"status"`
	Resource     json.RawMessage `json:"resource"`
	Scope        json.RawMessage `json:"scope"`
	Detail       json.RawMessage `json:"detail"`
}

func spanKey(s Span) string { return s.TraceID + "/" + s.SpanID }

func readSpans(j *observepipe.Journal) ([]Span, error) {
	records, err := j.Records("spans")
	if err != nil {
		return nil, err
	}
	spans := []Span{}
	seen := map[string]bool{}
	for _, raw := range records {
		var s, verified Span
		if json.Unmarshal(raw, &s) != nil || !validHex(s.TraceID, 16) || !validHex(s.SpanID, 8) {
			return nil, errors.New("invalid span identity")
		}
		if err := j.ReadRecord("spans", spanKey(s), &verified); err != nil {
			return nil, err
		}
		a, _ := json.Marshal(verified)
		b, _ := json.Marshal(s)
		if !bytes.Equal(a, b) || seen[spanKey(s)] {
			return nil, errors.New("span archive identity mismatch")
		}
		seen[spanKey(s)] = true
		spans = append(spans, s)
	}
	sort.Slice(spans, func(i, j int) bool {
		if spans[i].Start.Equal(spans[j].Start) {
			return spanKey(spans[i]) < spanKey(spans[j])
		}
		return spans[i].Start.Before(spans[j].Start)
	})
	return spans, nil
}

func validHex(s string, size int) bool {
	b, err := hex.DecodeString(s)
	return err == nil && len(b) == size && !bytes.Equal(b, make([]byte, size))
}

// OTLP JSON uses hexadecimal span/trace IDs, unlike protobuf JSON bytes.
func normalizeIDs(value any, toBase64 bool) error {
	switch v := value.(type) {
	case map[string]any:
		for k, child := range v {
			if k == "traceId" || k == "spanId" || k == "parentSpanId" {
				s, ok := child.(string)
				if !ok {
					return errors.New("invalid OTLP identity")
				}
				if s == "" {
					continue
				}
				var b []byte
				var err error
				if toBase64 {
					b, err = hex.DecodeString(s)
				} else {
					b, err = base64.StdEncoding.DecodeString(s)
				}
				size := 8
				if k == "traceId" {
					size = 16
				}
				if err != nil || len(b) != size {
					return errors.New("invalid OTLP identity length")
				}
				if toBase64 {
					v[k] = base64.StdEncoding.EncodeToString(b)
				} else {
					v[k] = hex.EncodeToString(b)
				}
			} else if err := normalizeIDs(child, toBase64); err != nil {
				return err
			}
		}
	case []any:
		for _, child := range v {
			if err := normalizeIDs(child, toBase64); err != nil {
				return err
			}
		}
	}
	return nil
}

func decodeOTLP(body []byte, jsonFormat bool) ([]Span, error) {
	request := new(collector.ExportTraceServiceRequest)
	if jsonFormat {
		var value any
		decoder := json.NewDecoder(bytes.NewReader(body))
		decoder.UseNumber()
		if err := decoder.Decode(&value); err != nil {
			return nil, err
		}
		if decoder.Decode(new(any)) != io.EOF {
			return nil, errors.New("trailing OTLP JSON content")
		}
		if err := normalizeIDs(value, true); err != nil {
			return nil, err
		}
		canonical, _ := json.Marshal(value)
		if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(canonical, request); err != nil {
			return nil, err
		}
	} else if err := proto.Unmarshal(body, request); err != nil {
		return nil, err
	}
	spans := []Span{}
	for _, resource := range request.ResourceSpans {
		if resource == nil {
			return nil, errors.New("missing resource spans")
		}
		service := "unknown"
		for _, attr := range resource.GetResource().GetAttributes() {
			if attr.GetKey() == "service.name" {
				service = attr.GetValue().GetStringValue()
			}
		}
		resourceJSON, _ := protojson.Marshal(resource.GetResource())
		for _, scope := range resource.ScopeSpans {
			if scope == nil {
				return nil, errors.New("missing scope spans")
			}
			scopeJSON, _ := protojson.Marshal(scope.GetScope())
			for _, item := range scope.Spans {
				if item == nil {
					return nil, errors.New("missing span")
				}
				s := Span{TraceID: hex.EncodeToString(item.TraceId), SpanID: hex.EncodeToString(item.SpanId), ParentSpanID: hex.EncodeToString(item.ParentSpanId), Name: item.Name, Service: service, Resource: resourceJSON, Scope: scopeJSON, Status: item.GetStatus().GetCode().String()}
				if !validHex(s.TraceID, 16) || !validHex(s.SpanID, 8) || (len(item.ParentSpanId) != 0 && len(item.ParentSpanId) != 8) || item.StartTimeUnixNano == 0 || item.EndTimeUnixNano < item.StartTimeUnixNano || item.EndTimeUnixNano > 1<<63-1 {
					return nil, errors.New("invalid span identity or time range")
				}
				s.Start = time.Unix(0, int64(item.StartTimeUnixNano)).UTC()
				s.End = time.Unix(0, int64(item.EndTimeUnixNano)).UTC()
				s.DurationMS = float64(item.EndTimeUnixNano-item.StartTimeUnixNano) / 1e6
				detail, _ := protojson.Marshal(item)
				var value any
				d := json.NewDecoder(bytes.NewReader(detail))
				d.UseNumber()
				_ = d.Decode(&value)
				if err := normalizeIDs(value, false); err != nil {
					return nil, err
				}
				s.Detail, _ = json.Marshal(value)
				spans = append(spans, s)
				if len(spans) > 10000 {
					return nil, errors.New("too many spans in request")
				}
			}
		}
	}
	return spans, nil
}

// Called under the server mutation lock. Validate the whole batch before writes;
// a storage failure can be retried with stable IDs without duplicate records.
func storeSpans(j *observepipe.Journal, spans []Span) error {
	seen := map[string][]byte{}
	for _, s := range spans {
		body, _ := json.Marshal(s)
		key := spanKey(s)
		if previous, ok := seen[key]; ok && !bytes.Equal(body, previous) {
			return fmt.Errorf("conflicting span identity")
		}
		seen[key] = body
		var old Span
		err := j.ReadRecord("spans", key, &old)
		if err == nil {
			previous, _ := json.Marshal(old)
			if !bytes.Equal(body, previous) {
				return fmt.Errorf("conflicting span identity")
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	for _, s := range spans {
		if err := j.PutRecord("spans", spanKey(s), s); err != nil {
			return err
		}
	}
	return nil
}
