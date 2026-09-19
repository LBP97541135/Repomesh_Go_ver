// Package observepipe is the out-of-process observation/evaluation adapter.
// Product use cases must not import this package or its telemetry dependencies.
package observepipe

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

const EventSchema = "repomesh-observe/0.1"

// Event is a persisted, vendor-neutral projection. Source payloads remain in
// evidence files; only an explicit attribute allowlist is sent to OTLP.
type Event struct {
	SchemaVersion      string    `json:"schema_version"`
	EventID            string    `json:"event_id"`
	EventName          string    `json:"event_name"`
	WorkID             string    `json:"work_id"`
	Producer           string    `json:"producer"`
	ProducerVersion    string    `json:"producer_version"`
	SourceEventKey     string    `json:"source_event_key"`
	ActorType          string    `json:"actor_type"`
	ActorID            string    `json:"actor_id"`
	EvidenceLevel      string    `json:"evidence_level"`
	OccurredAt         time.Time `json:"occurred_at"`
	RecordedAt         time.Time `json:"recorded_at"`
	TraceID            string    `json:"trace_id"`
	SpanID             string    `json:"span_id"`
	ProjectID          string    `json:"project_id,omitempty"`
	IssueID            string    `json:"issue_id,omitempty"`
	TrialID            string    `json:"trial_id,omitempty"`
	CaseID             string    `json:"case_id,omitempty"`
	VariantID          string    `json:"variant_id,omitempty"`
	CheckID            string    `json:"check_id,omitempty"`
	CausedByEventIDs   []string  `json:"caused_by_event_ids"`
	InputArtifactRefs  []string  `json:"input_artifact_refs"`
	ContextManifestRef string    `json:"context_manifest_ref"`
	EvidenceRefs       []string  `json:"evidence_refs"`
	ExecutionStatus    string    `json:"execution_status"`
	OutcomeVerdict     string    `json:"outcome_verdict"`
	ErrorClass         *string   `json:"error_class"`
	ReasonCode         *string   `json:"reason_code"`
	CollectionStatus   string    `json:"collection_status"`
	MissingFields      []string  `json:"missing_fields"`
	MissingReason      *string   `json:"missing_reason"`
}

func Digest(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }

func NewEvent(producer, version, sourceKey, name, work string, occurred time.Time) Event {
	// Use the standard SDK's identity generator; persist IDs before exporting so
	// a retry doesn't invent a new trace for the same source fact.
	provider := sdktrace.NewTracerProvider()
	_, span := provider.Tracer("repomesh-observe").Start(context.Background(), name)
	sc := span.SpanContext()
	span.End()
	_ = provider.Shutdown(context.Background())
	return Event{SchemaVersion: EventSchema, EventID: Digest([]byte(producer + "\x00" + sourceKey)),
		EventName: name, WorkID: work, Producer: producer, ProducerVersion: version,
		SourceEventKey: sourceKey, ActorType: "service", ActorID: producer,
		EvidenceLevel: "observed", OccurredAt: occurred.UTC(), RecordedAt: time.Now().UTC(),
		TraceID: sc.TraceID().String(), SpanID: sc.SpanID().String(),
		CausedByEventIDs: []string{}, InputArtifactRefs: []string{}, EvidenceRefs: []string{},
		ExecutionStatus: "completed", OutcomeVerdict: "not_evaluated", CollectionStatus: "partial", MissingFields: []string{}}
}

func (e Event) Validate() error {
	if e.SchemaVersion != EventSchema {
		return errors.New("unsupported event schema")
	}
	for _, s := range []string{e.EventName, e.WorkID, e.Producer, e.ProducerVersion, e.SourceEventKey, e.ActorType, e.ActorID, e.ContextManifestRef} {
		if strings.TrimSpace(s) == "" {
			return errors.New("event is missing required provenance")
		}
	}
	if e.EventID != Digest([]byte(e.Producer+"\x00"+e.SourceEventKey)) {
		return errors.New("event identity does not match its source")
	}
	tid, err := trace.TraceIDFromHex(e.TraceID)
	if err != nil || !tid.IsValid() {
		return errors.New("invalid trace identity")
	}
	sid, err := trace.SpanIDFromHex(e.SpanID)
	if err != nil || !sid.IsValid() {
		return errors.New("invalid span identity")
	}
	if e.OccurredAt.IsZero() || e.RecordedAt.IsZero() {
		return errors.New("event timestamps are required")
	}
	if !oneOf(e.ExecutionStatus, "pending", "running", "completed", "failed", "timed_out", "budget_exceeded", "cancelled", "unknown") ||
		!oneOf(e.OutcomeVerdict, "not_evaluated", "pass", "fail", "unknown", "not_applicable") ||
		!oneOf(e.CollectionStatus, "complete", "partial", "unknown") || !oneOf(e.EvidenceLevel, "observed", "reported", "inferred") {
		return errors.New("unsupported event status")
	}
	if e.CollectionStatus != "complete" && (len(e.MissingFields) == 0 || e.MissingReason == nil) {
		return errors.New("incomplete event needs explicit missing evidence")
	}
	return nil
}

func oneOf(s string, allowed ...string) bool {
	for _, a := range allowed {
		if s == a {
			return true
		}
	}
	return false
}

// semanticFingerprint omits collection-time metadata, never source facts.
func (e Event) semanticFingerprint() string {
	e.TraceID = ""
	e.SpanID = ""
	e.RecordedAt = time.Time{}
	b, _ := json.Marshal(e)
	return Digest(b)
}

func StringPtr(s string) *string { return &s }
