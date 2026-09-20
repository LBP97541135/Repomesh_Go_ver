package observeui

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"repomesh.local/repomesh/internal/observepipe"
)

type Call struct {
	ResultStatus string `json:"result_status,omitempty"`

	Archive            string    `json:"archive"`
	SourceID           string    `json:"source_id"`
	CallID             string    `json:"call_id"`
	TraceID            string    `json:"trace_id"`
	SpanID             string    `json:"span_id"`
	ParentSpanID       string    `json:"parent_span_id"`
	Name               string    `json:"name"`
	Kind               string    `json:"kind"`
	Model              string    `json:"model"`
	Tool               string    `json:"tool"`
	ProjectID          string    `json:"project_id"`
	IssueID            string    `json:"issue_id"`
	TaskID             string    `json:"task_id"`
	AttemptID          string    `json:"attempt_id"`
	RepositoryID       string    `json:"repository_id"`
	Role               string    `json:"role"`
	Purpose            string    `json:"execution_purpose"`
	SubjectKind        string    `json:"subject_kind"`
	BindingStatus      string    `json:"binding_status"`
	Start              time.Time `json:"start"`
	DurationMS         float64   `json:"duration_ms"`
	TTFTMS             *float64  `json:"ttft_ms"`
	TTFTReason         string    `json:"ttft_reason"`
	FirstOutputKind    string    `json:"first_output_kind"`
	InputTokens        *int64    `json:"input_tokens"`
	OutputTokens       *int64    `json:"output_tokens"`
	CacheReadTokens    *int64    `json:"cache_read_tokens"`
	ReasoningTokens    *int64    `json:"reasoning_tokens"`
	Status             string    `json:"status"`
	Missing            []string  `json:"missing_fields"`
	RetryOf            string    `json:"retry_of"`
	LogicalOperationID string    `json:"logical_operation_id"`
}

func attributes(raw json.RawMessage) map[string]any {
	var container struct {
		Attributes []struct {
			Key   string                     `json:"key"`
			Value map[string]json.RawMessage `json:"value"`
		} `json:"attributes"`
	}
	out := map[string]any{}
	if json.Unmarshal(raw, &container) != nil {
		return out
	}
	for _, a := range container.Attributes {
		for kind, value := range a.Value {
			switch kind {
			case "stringValue":
				var s string
				if json.Unmarshal(value, &s) == nil {
					out[a.Key] = s
				}
			case "intValue":
				var s string
				if json.Unmarshal(value, &s) == nil {
					if n, e := strconv.ParseInt(s, 10, 64); e == nil {
						out[a.Key] = n
					}
				} else {
					var n int64
					if json.Unmarshal(value, &n) == nil {
						out[a.Key] = n
					}
				}
			case "doubleValue":
				var n float64
				if json.Unmarshal(value, &n) == nil {
					out[a.Key] = n
				}
			case "boolValue":
				var b bool
				if json.Unmarshal(value, &b) == nil {
					out[a.Key] = b
				}
			}
		}
	}
	return out
}

func str(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k].(string); ok && v != "" {
			return v
		}
	}
	return ""
}
func number(m map[string]any, keys ...string) *float64 {
	for _, k := range keys {
		var v float64
		switch n := m[k].(type) {
		case int64:
			v = float64(n)
		case float64:
			v = n
		default:
			continue
		}
		if v >= 0 && !math.IsInf(v, 0) && !math.IsNaN(v) {
			return &v
		}
	}
	return nil
}
func integer(m map[string]any, keys ...string) *int64 {
	for _, k := range keys {
		if n, ok := m[k].(int64); ok && n >= 0 {
			return &n
		}
		if n, ok := m[k].(float64); ok && n >= 0 && n < 9e15 && math.Trunc(n) == n {
			v := int64(n)
			return &v
		}
	}
	return nil
}

type TraceBinding struct {
	TraceID      string `json:"trace_id"`
	SourceID     string `json:"source_id"`
	ProjectID    string `json:"project_id"`
	IssueID      string `json:"issue_id"`
	TaskID       string `json:"task_id"`
	AttemptID    string `json:"attempt_id"`
	RepositoryID string `json:"repository_id"`
	EvidenceRef  string `json:"evidence_ref"`
	Origin       string `json:"origin"`
}

func bindingKey(b TraceBinding) string { return b.SourceID + ":" + b.TraceID }

func (s *Server) binding(source, trace string) (*TraceBinding, error) {
	var found *TraceBinding
	for _, a := range s.archives {
		var b TraceBinding
		err := a.Journal.ReadRecord("bindings", source+":"+trace, &b)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		var evidence any
		if b.TraceID != trace || b.SourceID != source || a.Journal.ReadEvidence(b.EvidenceRef, &evidence) != nil {
			return nil, errors.New("invalid binding provenance")
		}
		if found != nil {
			x, _ := json.Marshal(found)
			y, _ := json.Marshal(b)
			if string(x) != string(y) {
				return nil, errors.New("conflicting binding")
			}
		}
		copy := b
		found = &copy
	}
	return found, nil
}

func (s *Server) projectCall(a archive, span Span) (Call, bool, error) {
	m := attributes(span.Resource)
	for k, v := range attributes(span.Detail) {
		m[k] = v
	}
	kind := str(m, "gen_ai.span.kind")
	op := str(m, "gen_ai.operation.name")
	if kind == "" {
		switch op {
		case "chat", "generate_content", "text_completion":
			kind = "LLM"
		case "execute_tool":
			kind = "TOOL"
		case "invoke_agent":
			kind = "AGENT"
		}
	}
	if str(m, "repomesh.duration_kind") == "instantaneous_fact" || kind == "" {
		return Call{}, false, nil
	}
	c := Call{Archive: a.ID, SourceID: str(m, "repomesh.source_id", "service.instance.id"), CallID: str(m, "repomesh.call_id", "gen_ai.response.id"), TraceID: span.TraceID, SpanID: span.SpanID, ParentSpanID: span.ParentSpanID, Name: span.Name, Kind: kind, Model: str(m, "gen_ai.response.model", "gen_ai.request.model"), Tool: str(m, "gen_ai.tool.name"), ProjectID: str(m, "repomesh.project_id"), IssueID: str(m, "repomesh.issue_id"), TaskID: str(m, "repomesh.task_id"), AttemptID: str(m, "repomesh.attempt_id"), RepositoryID: str(m, "repomesh.repository_id"), Role: str(m, "repomesh.role", "gen_ai.agent.name"), Purpose: str(m, "repomesh.execution_purpose"), SubjectKind: str(m, "repomesh.subject_kind"), BindingStatus: "unresolved", Start: span.Start, DurationMS: span.DurationMS, TTFTReason: "not_recorded", InputTokens: integer(m, "gen_ai.usage.input_tokens"), OutputTokens: integer(m, "gen_ai.usage.output_tokens"), CacheReadTokens: integer(m, "gen_ai.usage.cache_read.input_tokens"), ReasoningTokens: integer(m, "gen_ai.usage.reasoning_tokens"), Status: span.Status, Missing: []string{}, RetryOf: str(m, "repomesh.retry_of"), LogicalOperationID: str(m, "repomesh.logical_operation_id"), FirstOutputKind: str(m, "repomesh.first_output_kind")}
	if c.SourceID == "" {
		c.SourceID = "unidentified:" + span.Service
		c.Missing = append(c.Missing, "source_id")
	}
	if c.CallID == "" {
		c.CallID = span.TraceID + ":" + span.SpanID
	}
	if c.Purpose == "" {
		c.Purpose = "unknown"
		c.Missing = append(c.Missing, "execution_purpose")
	}
	if c.SubjectKind == "" {
		c.SubjectKind = "unverified"
	}
	if c.ProjectID != "" || c.IssueID != "" {
		c.BindingStatus = "reported"
	}
	b, err := s.binding(c.SourceID, c.TraceID)
	if err != nil {
		return c, false, err
	}
	if b != nil {
		for _, pair := range [][2]string{{c.ProjectID, b.ProjectID}, {c.IssueID, b.IssueID}, {c.TaskID, b.TaskID}, {c.AttemptID, b.AttemptID}} {
			if pair[0] != "" && pair[0] != pair[1] {
				return c, false, errors.New("span conflicts with registered binding")
			}
		}
		c.ProjectID = b.ProjectID
		c.IssueID = b.IssueID
		c.TaskID = b.TaskID
		c.AttemptID = b.AttemptID
		if b.RepositoryID != "" {
			if c.RepositoryID != "" && c.RepositoryID != b.RepositoryID {
				return c, false, errors.New("span repository conflicts with registered binding")
			}
			c.RepositoryID = b.RepositoryID
		}
		c.BindingStatus = "exact"
	}
	if kind == "LLM" {
		ns := number(m, "repomesh.ttft_ns")
		if ns == nil && str(m, "repomesh.ttft_unit") == "ns" {
			ns = number(m, "gen_ai.response.time_to_first_token")
		}
		streaming, streamKnown := m["repomesh.streaming"].(bool)
		if streamKnown && !streaming {
			c.TTFTReason = "not_supported"
		} else if !streamKnown || !oneOfStrings(c.FirstOutputKind, "reasoning", "text", "tool_call") {
			c.TTFTReason = "semantics_not_recorded"
		} else if ns != nil {
			v := *ns / 1e6
			if v <= c.DurationMS {
				c.TTFTMS = &v
				c.TTFTReason = "observed"
			} else {
				c.TTFTReason = "invalid_range"
			}
		}

		if c.InputTokens == nil {
			c.Missing = append(c.Missing, "input_tokens")
		}
		if c.OutputTokens == nil {
			c.Missing = append(c.Missing, "output_tokens")
		}
		if c.TTFTMS == nil {
			c.Missing = append(c.Missing, "ttft")
		}
	}
	if c.BindingStatus != "exact" {
		c.Missing = append(c.Missing, "exact_business_binding")
	}
	return c, true, nil
}

type metricTotals struct {
	Calls         int      `json:"calls"`
	InputTokens   int64    `json:"known_input_tokens"`
	OutputTokens  int64    `json:"known_output_tokens"`
	MeasuredUsage int      `json:"measured_usage_calls"`
	MeasuredTTFT  int      `json:"measured_ttft_calls"`
	MeanTTFT      *float64 `json:"mean_ttft_ms"`
	MeanDuration  *float64 `json:"mean_duration_ms"`
	ExactBindings int      `json:"exact_bindings"`
	Errors        int      `json:"errors"`
}

func (s *Server) calls() ([]Call, []map[string]string) {
	out := []Call{}
	faults := []map[string]string{}
	seen := map[string]Call{}
	conflicts := map[string]bool{}
	for _, a := range s.archives {
		spans, err := readSpans(a.Journal)
		if err != nil {
			faults = append(faults, map[string]string{"archive": a.ID, "error": "span archive integrity failure"})
			continue
		}
		for _, span := range spans {
			c, ok, err := s.projectCall(a, span)
			if err != nil {
				faults = append(faults, map[string]string{"archive": a.ID, "trace_id": span.TraceID, "error": err.Error()})
				continue
			}
			if !ok {
				continue
			}
			key := c.SourceID + ":" + c.CallID
			if old, exists := seen[key]; exists {
				left, right := old, c
				left.Archive = ""
				right.Archive = ""
				x, _ := json.Marshal(left)
				y, _ := json.Marshal(right)
				if string(x) != string(y) {
					conflicts[key] = true
					faults = append(faults, map[string]string{"archive": a.ID, "error": "conflicting physical call: " + c.CallID})
				}
				continue
			}
			seen[key] = c
			out = append(out, c)
		}
	}

	for _, a := range s.archives {
		records, err := a.Journal.Records("model_calls")
		if err != nil {
			faults = append(faults, map[string]string{"archive": a.ID, "error": "model call archive corrupt"})
			continue
		}
		for _, raw := range records {
			var record, verified observepipe.RecordedModelCall
			if json.Unmarshal(raw, &record) != nil || record.Schema != "repomesh.model-call/1" || record.SourceID == "" || record.Call.ID == "" || a.Journal.ReadRecord("model_calls", record.SourceID+":"+record.Call.ID, &verified) != nil || !jsonEqual(record, verified) {
				faults = append(faults, map[string]string{"archive": a.ID, "error": "invalid model call identity"})
				continue
			}
			m := record.Call
			c := Call{ResultStatus: m.ResultStatus, Archive: a.ID, SourceID: record.SourceID, CallID: m.ID, Name: "discovery.semantic_recall", Kind: "LLM", Model: m.ResponseModel, ProjectID: m.ProjectID, IssueID: m.IssueID, Purpose: "team", SubjectKind: "observed_product_call", BindingStatus: "exact_issue", Start: m.StartedAt, DurationMS: float64(m.DurationNS) / 1e6, TTFTReason: "not_supported", InputTokens: m.InputTokens, OutputTokens: m.OutputTokens, CacheReadTokens: m.CacheReadTokens, Status: m.Status, Missing: []string{"task_id", "attempt_id", "runtime_turn", "ttft_non_streaming"}}
			if c.Model == "" {
				c.Model = m.RequestedModel
			}
			if c.InputTokens == nil {
				c.Missing = append(c.Missing, "input_tokens")
			}
			if c.OutputTokens == nil {
				c.Missing = append(c.Missing, "output_tokens")
			}
			key := c.SourceID + ":" + c.CallID
			if old, ok := seen[key]; ok {
				left, right := old, c
				left.Archive = ""
				right.Archive = ""
				if !jsonEqual(left, right) {
					conflicts[key] = true
					faults = append(faults, map[string]string{"archive": a.ID, "error": "model call identity conflict"})
				}
				continue
			}
			seen[key] = c
			out = append(out, c)
		}
		judgments, err := readJudgments(a.Journal)
		if err != nil {
			continue
		}
		for _, g := range judgments {
			if g.ExecutionPurpose != "judge" {
				continue
			}
			var usage struct {
				Input  *int64 `json:"input_tokens"`
				Output *int64 `json:"output_tokens"`
			}
			_ = json.Unmarshal(g.Usage, &usage)
			c := Call{Archive: a.ID, SourceID: "local-judge", CallID: g.ID, Name: "rubric.evaluate", Kind: "LLM", Model: g.Model, Purpose: "judge", SubjectKind: "evaluation", BindingStatus: "not_applicable", Start: g.CreatedAt, DurationMS: g.DurationMS, TTFTReason: "not_supported", InputTokens: usage.Input, OutputTokens: usage.Output, Status: g.Status, Missing: []string{"ttft_non_streaming"}}
			key := c.SourceID + ":" + c.CallID
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = c
			out = append(out, c)
		}
	}
	for _, a := range s.archives {
		records, err := a.Journal.Records("analyses")
		if err != nil {
			faults = append(faults, map[string]string{"archive": a.ID, "error": "analysis usage unavailable"})
			continue
		}
		for _, raw := range records {
			var row, verified LocalAnalysis
			if json.Unmarshal(raw, &row) != nil || a.Journal.ReadRecord("analyses", row.ID, &verified) != nil || !jsonEqual(row, verified) {
				faults = append(faults, map[string]string{"archive": a.ID, "error": "invalid analysis identity"})
				continue
			}
			var usage struct {
				Input  *int64 `json:"prompt_tokens"`
				Output *int64 `json:"completion_tokens"`
			}
			_ = json.Unmarshal(row.Usage, &usage)
			c := Call{Archive: a.ID, SourceID: "local-analysis", CallID: row.ID, Name: row.Kind, Kind: "LLM", Model: row.Model, Purpose: "analysis", SubjectKind: "evaluation", BindingStatus: "not_applicable", Start: row.CreatedAt, DurationMS: row.DurationMS, TTFTReason: "not_supported", InputTokens: usage.Input, OutputTokens: usage.Output, Status: row.Status, Missing: []string{"ttft_non_streaming"}}
			key := c.SourceID + ":" + c.CallID
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = c
			out = append(out, c)
		}
	}

	filtered := out[:0]
	for _, c := range out {
		if !conflicts[c.SourceID+":"+c.CallID] {
			filtered = append(filtered, c)
		}
	}
	out = filtered
	sort.Slice(out, func(i, j int) bool {
		if out[i].Start.Equal(out[j].Start) {
			return out[i].SourceID+out[i].CallID < out[j].SourceID+out[j].CallID
		}
		return out[i].Start.After(out[j].Start)
	})
	return out, faults
}

func filterCalls(calls []Call, r *http.Request) []Call {
	out := []Call{}
	q := r.URL.Query()
	for _, c := range calls {
		match := true
		for key, value := range map[string]string{"project": c.ProjectID, "issue": c.IssueID, "task": c.TaskID, "attempt": c.AttemptID, "repository": c.RepositoryID, "role": c.Role, "model": c.Model, "purpose": c.Purpose, "kind": c.Kind, "trace": c.TraceID, "subject_kind": c.SubjectKind, "binding": c.BindingStatus} {
			if f := q.Get(key); f != "" && f != value {
				match = false
			}
		}
		if text := strings.ToLower(q.Get("q")); text != "" && !strings.Contains(strings.ToLower(c.Name+" "+c.Model+" "+c.Tool+" "+c.IssueID+" "+c.TaskID+" "+c.AttemptID+" "+c.TraceID), text) {
			match = false
		}
		if match {
			out = append(out, c)
		}
	}
	return out
}

func (s *Server) queryCalls(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	calls, faults := s.calls()
	calls = filterCalls(calls, r)
	limit := 100
	if v, e := strconv.Atoi(r.URL.Query().Get("limit")); e == nil && v > 0 && v <= 500 {
		limit = v
	}
	offset := 0
	if v, e := strconv.Atoi(r.URL.Query().Get("offset")); e == nil && v >= 0 {
		offset = v
	}
	total := len(calls)
	if offset > total {
		offset = total
	}
	end := min(offset+limit, total)
	writeJSON(w, 200, map[string]any{"calls": calls[offset:end], "total": total, "offset": offset, "next_offset": end, "has_more": end < total, "errors": faults, "mapping_version": "repomesh-genai/1"})
}

func (s *Server) metrics(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	calls, faults := s.calls()
	calls = filterCalls(calls, r)
	groups := map[string]*metricTotals{}
	ttft := map[string]float64{}
	duration := map[string]float64{}
	for _, c := range calls {
		if c.Kind != "LLM" {
			continue
		}
		key := c.SubjectKind + " / " + c.Purpose + " / " + c.Model + " / first:" + c.FirstOutputKind
		g := groups[key]
		if g == nil {
			g = &metricTotals{}
			groups[key] = g
		}
		g.Calls++
		duration[key] += c.DurationMS
		if c.InputTokens != nil {
			g.InputTokens += *c.InputTokens
		}
		if c.OutputTokens != nil {
			g.OutputTokens += *c.OutputTokens
		}
		if c.InputTokens != nil && c.OutputTokens != nil {
			g.MeasuredUsage++
		}
		if c.TTFTMS != nil {
			g.MeasuredTTFT++
			ttft[key] += *c.TTFTMS
		}
		if c.BindingStatus == "exact" {
			g.ExactBindings++
		}
		if c.Status == "STATUS_CODE_ERROR" || c.Status == "error" || c.ResultStatus == "invalid_output" {
			g.Errors++
		}
	}
	for k, g := range groups {
		v := duration[k] / float64(g.Calls)
		g.MeanDuration = &v
		if g.MeasuredTTFT > 0 {
			v := ttft[k] / float64(g.MeasuredTTFT)
			g.MeanTTFT = &v
		}
	}
	writeJSON(w, 200, map[string]any{"groups": groups, "observed_calls": len(calls), "collection_coverage": nil, "coverage_reason": "independent source denominator not available", "errors": faults, "cost": nil, "cost_reason": "versioned price or billing source not configured", "aggregation": "deduplicated physical calls; known token subtotal only"})
}

func (s *Server) registerBinding(w http.ResponseWriter, r *http.Request) {
	var p struct {
		Binding  TraceBinding    `json:"binding"`
		Evidence json.RawMessage `json:"evidence"`
	}
	if !readJSON(w, r, &p) {
		return
	}
	b := p.Binding
	if !validHex(b.TraceID, 16) || b.SourceID == "" || b.ProjectID == "" || b.IssueID == "" || b.TaskID == "" || b.AttemptID == "" || len(p.Evidence) == 0 || string(p.Evidence) == "null" {
		failHTTP(w, 400, "exact binding requires source, task, attempt and dispatch evidence")
		return
	}
	b.Origin = "local_operator_registered"
	s.mu.Lock()
	defer s.mu.Unlock()
	ref, err := s.archives[0].Journal.PutRawJSON(p.Evidence)
	if err != nil {
		failHTTP(w, 400, "invalid binding evidence")
		return
	}
	b.EvidenceRef = ref
	if old, err := s.binding(b.SourceID, b.TraceID); err != nil {
		failHTTP(w, 409, "binding integrity failure")
		return
	} else if old != nil {
		left, _ := json.Marshal(old)
		right, _ := json.Marshal(b)
		if string(left) != string(right) {
			failHTTP(w, 409, "binding is immutable")
			return
		}
	}
	if err := s.archives[0].Journal.PutRecord("bindings", bindingKey(b), b); err != nil {
		failHTTP(w, 409, "binding conflict or storage failure")
		return
	}
	writeJSON(w, 201, b)
}

func traceRevision(spans []Span) string {
	sort.Slice(spans, func(i, j int) bool { return spanKey(spans[i]) < spanKey(spans[j]) })
	b, _ := json.Marshal(spans)
	return observepipe.Digest(b)
}

func (s *Server) traceDetails(w http.ResponseWriter, r *http.Request) {
	a := s.findArchive(r.URL.Query().Get("archive"))
	if a == nil {
		failHTTP(w, 404, "archive not found")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	trace := r.URL.Query().Get("id")
	all, err := readSpans(a.Journal)
	if err != nil {
		failHTTP(w, 409, "trace archive corrupt")
		return
	}
	spans := []Span{}
	for _, sp := range all {
		if sp.TraceID == trace {
			spans = append(spans, sp)
		}
	}
	if len(spans) == 0 {
		failHTTP(w, 404, "trace not found")
		return
	}
	material, available, revision, inputErr := s.traceMaterial(a, trace)
	writeJSON(w, 200, map[string]any{"spans": spans, "revision": traceRevision(spans), "material": material, "available": available, "grading_eligible": inputErr == nil && revision != "" && material["binding_status"] == "exact"})
}

func (s *Server) traceMaterial(a *archive, trace string) (map[string]any, map[string]bool, string, error) {
	all, err := readSpans(a.Journal)
	if err != nil {
		return nil, nil, "", err
	}
	spans := []Span{}
	tools := []any{}
	messages := []any{}
	missing := []string{}
	hasJudge := false
	kind := "unverified"
	for _, span := range all {
		if span.TraceID != trace {
			continue
		}
		spans = append(spans, span)
		m := attributes(span.Detail)
		if p := str(m, "repomesh.execution_purpose"); p != "" {
			if p == "judge" {
				hasJudge = true
			}
		}
		if k := str(m, "repomesh.subject_kind"); k != "" {
			kind = k
		}
		if str(m, "gen_ai.span.kind") == "TOOL" || str(m, "gen_ai.operation.name") == "execute_tool" {
			tools = append(tools, map[string]any{"tool": str(m, "gen_ai.tool.name"), "arguments": str(m, "gen_ai.tool.call.arguments", "input.value"), "result": str(m, "gen_ai.tool.call.result", "output.value"), "status": span.Status, "duration_ms": span.DurationMS})
		}
		for _, key := range []string{"gen_ai.input.messages", "gen_ai.output.messages", "input.value", "output.value"} {
			if v := str(m, key); v != "" {
				messages = append(messages, map[string]string{"field": key, "content": v})
			}
		}
	}
	if len(spans) == 0 {
		return nil, nil, "", errors.New("trace not found")
	}
	if hasJudge {
		return nil, nil, "", errors.New("judge traces are excluded from evaluation")
	}
	ids := map[string]bool{}
	for _, sp := range spans {
		ids[sp.SpanID] = true
	}
	for _, sp := range spans {
		if sp.ParentSpanID != "" && !ids[sp.ParentSpanID] {
			missing = append(missing, "parent:"+sp.ParentSpanID)
		}
	}
	// Tools alone do not prove the entire trajectory was collected. Producers
	// must explicitly mark a terminal complete execution before efficiency grading.
	complete := false
	for _, sp := range spans {
		if sp.ParentSpanID != "" {
			continue
		}
		m := attributes(sp.Detail)
		expected := integer(m, "repomesh.expected_span_count")
		if expected != nil && *expected == int64(len(spans)) && str(m, "repomesh.collection_status") == "complete" && str(m, "repomesh.execution_status") == "completed" {
			complete = true
		}
	}
	bindingState := "unresolved"
	var binding *TraceBinding
	for _, sp := range spans {
		attrs := attributes(sp.Resource)
		for k, v := range attributes(sp.Detail) {
			attrs[k] = v
		}
		source := str(attrs, "repomesh.source_id", "service.instance.id")
		if source != "" {
			candidate, err := s.binding(source, trace)
			if err != nil {
				return nil, nil, "", err
			}
			if candidate != nil {
				if binding != nil && !jsonEqual(binding, candidate) {
					return nil, nil, "", errors.New("conflicting trace bindings")
				}
				binding = candidate
				bindingState = "exact"
			}
		}
	}

	available := map[string]bool{"evidence": len(messages) > 0, "tool_trajectory": len(tools) > 0 && complete && len(missing) == 0 && bindingState == "exact"}
	return map[string]any{"trace_id": trace, "subject_kind": kind, "messages": messages, "tools": tools, "missing": missing, "complete": complete, "binding_status": bindingState, "binding": binding}, available, traceRevision(spans), nil
}

func safeExplanation(err error) string {
	var j *observepipe.JevError
	if errors.As(err, &j) {
		return j.Error()
	}
	return fmt.Sprint("evaluation input or storage unavailable")
}
