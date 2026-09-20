package observeui

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"repomesh.local/repomesh/internal/observepipe"
)

type mockRoundTrip func(*http.Request) (*http.Response, error)

func (f mockRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func fakeJev(t *testing.T, calls *atomic.Int64) *http.Client {
	t.Helper()
	return &http.Client{Transport: mockRoundTrip(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != "https://api.typesafe.ai/v1/systemone" {
			t.Errorf("unexpected model request %s", r.URL)
			return nil, context.Canceled
		}
		calls.Add(1)
		var input struct {
			Questions map[string]struct {
				Type     string          `json:"type"`
				Criteria json.RawMessage `json:"criteria"`
			} `json:"questions"`
			State any `json:"state"`
		}
		if json.NewDecoder(r.Body).Decode(&input) != nil {
			t.Fatal("invalid request")
		}
		answers := map[string]any{}
		for id, q := range input.Questions {
			if q.Type == "score" {
				var levels []string
				_ = json.Unmarshal(q.Criteria, &levels)
				legend := map[string]string{}
				probs := map[string]float64{}
				for i, level := range levels {
					k := string(rune('0' + i))
					legend[k] = level
					probs[k] = 0
				}
				probs[string(rune('0'+len(levels)-1))] = 1
				answers[id] = map[string]any{"type": "score", "score": len(levels) - 1, "confidence": 1, "legend": legend, "probabilities": probs}
			} else {
				answers[id] = map[string]any{"type": "choice", "choice": "sufficient", "confidence": 1, "probabilities": map[string]float64{"sufficient": 1, "insufficient": 0}}
			}
		}
		body, _ := json.Marshal(map[string]any{"model": "jev-1.13.0", "answers": answers, "usage": map[string]int{"input_tokens": 123, "output_tokens": 9}})
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(string(body))), Request: r}, nil
	})}
}

func makeSpan(t *testing.T, id, parent string, attrs map[string]any) Span {
	t.Helper()
	attributes := []map[string]any{}
	for k, v := range attrs {
		value := map[string]any{}
		switch n := v.(type) {
		case string:
			value["stringValue"] = n
		case bool:
			value["boolValue"] = n
		case int:
			value["intValue"] = n
		case int64:
			value["intValue"] = n
		}
		attributes = append(attributes, map[string]any{"key": k, "value": value})
	}
	detail, _ := json.Marshal(map[string]any{"attributes": attributes})
	return Span{TraceID: "11111111111111111111111111111111", SpanID: id, ParentSpanID: parent, Name: "test.call", Service: "test-service", Start: time.Date(2026, 9, 20, 1, 0, 0, 0, time.UTC), End: time.Date(2026, 9, 20, 1, 0, 1, 0, time.UTC), DurationMS: 1000, Status: "STATUS_CODE_OK", Resource: json.RawMessage(`{}`), Scope: json.RawMessage(`{}`), Detail: detail}
}

func TestJevScoresAreDurableAndNeverOverwriteVerification(t *testing.T) {
	s, h := newTestServer(t)
	status, _ := request(t, h, "PUT", "/api/model", map[string]string{"provider": "typesafe", "model": "jev-1.13.0", "api_key": "test-jev"})
	if status != 200 {
		t.Fatal(status)
	}
	var calls atomic.Int64
	s.client = fakeJev(t, &calls)
	status, _ = request(t, h, "POST", "/api/trials", map[string]string{"variant": "baseline"})
	if status != 201 {
		t.Fatal(status)
	}
	rows, _ := observepipe.DatasetRows(s.archives[0].Journal)
	status, body := request(t, h, "POST", "/api/judge", map[string]string{"archive": s.archives[0].ID, "trial_id": rows[0].TrialID})
	if status != 201 {
		t.Fatalf("%d %s", status, body)
	}
	var grade Judgment
	_ = json.Unmarshal(body, &grade)
	if grade.Provider != "typesafe" || grade.ExecutionPurpose != "judge" || grade.SubjectRevision == "" || len(grade.TypedAnswers) == 0 || grade.Status != "complete" {
		t.Fatal("typed provenance missing")
	}
	for _, d := range grade.Dimensions {
		if d.ID == "tool_efficiency" && (d.Score != nil || d.Verdict != "not_applicable") {
			t.Fatal("fixed HTTP product given an Agent efficiency score")
		}
	}
	rows, _ = observepipe.DatasetRows(s.archives[0].Journal)
	if rows[0].Verdict != "fail" {
		t.Fatal("Jev overwrote known business failure")
	}
	status, body = request(t, h, "GET", "/api/metrics", nil)
	if status != 200 || !strings.Contains(string(body), "judge") || !strings.Contains(string(body), "123") {
		t.Fatal("judge usage missing or not separate")
	}
	status, body = request(t, h, "GET", "/api/settings", nil)
	if status != 200 || strings.Contains(string(body), "test-jev") {
		t.Fatal("credential leaked")
	}
	if calls.Load() != 1 {
		t.Fatal("unexpected duplicate billing")
	}
}

func TestCallsPreserveMissingMetricsAndRequireExactBinding(t *testing.T) {
	s, h := newTestServer(t)
	span := makeSpan(t, "1111111111111111", "", map[string]any{"gen_ai.span.kind": "LLM", "gen_ai.request.model": "model-a", "repomesh.source_id": "deploy-a", "repomesh.call_id": "call-a", "repomesh.issue_id": "reported-issue", "repomesh.execution_purpose": "team", "gen_ai.usage.input_tokens": 100, "gen_ai.usage.output_tokens": 20, "repomesh.ttft_ns": int64(250000000), "repomesh.streaming": true, "repomesh.first_output_kind": "text"})
	if err := storeSpans(s.archives[0].Journal, []Span{span}); err != nil {
		t.Fatal(err)
	}
	status, body := request(t, h, "GET", "/api/calls?kind=LLM", nil)
	var result struct {
		Calls []Call `json:"calls"`
	}
	_ = json.Unmarshal(body, &result)
	if status != 200 || len(result.Calls) != 1 || result.Calls[0].BindingStatus != "reported" || *result.Calls[0].TTFTMS != 250 {
		t.Fatalf("bad metrics %s", body)
	}
	span2 := makeSpan(t, "2222222222222222", "", map[string]any{"gen_ai.span.kind": "LLM", "repomesh.source_id": "deploy-a", "repomesh.call_id": "call-b", "repomesh.streaming": false})
	if err := storeSpans(s.archives[0].Journal, []Span{span2}); err != nil {
		t.Fatal(err)
	}
	_, body = request(t, h, "GET", "/api/calls", nil)
	_ = json.Unmarshal(body, &result)
	for _, c := range result.Calls {
		if c.CallID == "call-b" && (c.TTFTMS != nil || c.InputTokens != nil || c.TTFTReason != "not_supported") {
			t.Fatal("missing values filled")
		}
	}
	status, _ = request(t, h, "POST", "/api/bindings", map[string]any{"binding": TraceBinding{TraceID: span.TraceID, SourceID: "deploy-a", ProjectID: "p", IssueID: "reported-issue", TaskID: "t", AttemptID: "a"}, "evidence": map[string]string{"dispatch": "trusted-local-fixture"}})
	if status != 201 {
		t.Fatal("binding failed")
	}
	_, body = request(t, h, "GET", "/api/calls?issue=reported-issue&binding=exact", nil)
	_ = json.Unmarshal(body, &result)
	if len(result.Calls) != 2 {
		t.Fatal("exact trace binding not inherited")
	}
}

func TestReadSourceSampleAndLocalLabelsNeverMutateSource(t *testing.T) {
	sourceDir := filepath.Join(t.TempDir(), "source")
	source, err := observepipe.OpenJournal(sourceDir)
	if err != nil {
		t.Fatal(err)
	}
	trial, err := observepipe.RunDiscountFixture(context.Background(), "candidate")
	if err != nil {
		t.Fatal(err)
	}
	archived, err := observepipe.ArchiveTrial(source, trial)
	if err != nil {
		t.Fatal(err)
	}
	s, err := New(Options{Archive: filepath.Join(t.TempDir(), "active"), ReadArchives: []string{sourceDir}})
	if err != nil {
		t.Fatal(err)
	}
	h := httptest.NewServer(s)
	defer h.Close()
	p := map[string]string{"archive": s.archives[1].ID, "trial_id": trial.TrialID, "expected_revision": archived.ReportRef, "reason": "high_score_audit"}
	status, body := request(t, h, "POST", "/api/samples", p)
	if status != 201 {
		t.Fatalf("%d %s", status, body)
	}
	var sample Sample
	_ = json.Unmarshal(body, &sample)
	status, body = request(t, h, "POST", "/api/samples", p)
	if status != 200 {
		t.Fatal("sample replay not idempotent")
	}
	label := map[string]string{"sample_id": sample.ID, "expected_revision": sample.SubjectRevision, "actor": "reviewer", "verdict": "pass", "category": "none", "reason": "Verified persisted amount against frozen requirement"}
	status, body = request(t, h, "POST", "/api/annotations", label)
	if status != 201 {
		t.Fatalf("label %d %s", status, body)
	}
	var annotation SampleAnnotation
	_ = json.Unmarshal(body, &annotation)
	if !annotation.Confirmed || annotation.ActorType != "human" {
		t.Fatal("human provenance missing")
	}
	label["expected_revision"] = "other-version"
	status, _ = request(t, h, "POST", "/api/annotations", label)
	if status != 409 {
		t.Fatal("stale evidence accepted")
	}
	if _, err := os.Stat(filepath.Join(sourceDir, "samples")); !os.IsNotExist(err) {
		t.Fatal("read source mutated")
	}
	if _, err := os.Stat(filepath.Join(sourceDir, "annotations")); !os.IsNotExist(err) {
		t.Fatal("source labels changed")
	}
	status, body = request(t, h, "GET", "/api/samples/export?id="+sample.ID, nil)
	if status != 200 || !strings.Contains(string(body), "8000") {
		t.Fatal("export lacks self-contained evidence")
	}
}

func TestLocalOnlyRejectsCloudAndAIHumanConfirmation(t *testing.T) {
	_, h := newTestServer(t)
	status, _ := request(t, h, "PUT", "/api/integration", map[string]any{"mode": "agentloop", "workspace": "cloud", "links": map[string]string{}})
	if status != 400 {
		t.Fatal("cloud mode enabled despite local-only constraint")
	}
	status, _ = request(t, h, "POST", "/api/analyses/clustering", map[string]any{"sample_ids": []string{"one"}})
	if status != 400 {
		t.Fatal("fabricated clusters from insufficient samples")
	}
	status, _ = request(t, h, "POST", "/api/annotations/import", map[string]any{"annotation": SampleAnnotation{Schema: "repomesh.annotation-import/1", Platform: "agentloop", DatasetID: "d", ItemID: "i", ExternalID: "e", TemplateVersion: "v", ActorID: "ai", ActorType: "ai", Verdict: "pass", Reason: "test", Confirmed: true, AnnotatedAt: time.Now()}, "raw": map[string]string{"export": "example"}})
	if status != 400 {
		t.Fatal("AI initial label counted as human")
	}
}

func TestOnlineEvaluationIsOptInDeduplicatedAndExcludesJudge(t *testing.T) {
	s, h := newTestServer(t)
	_, _ = request(t, h, "PUT", "/api/model", map[string]string{"provider": "typesafe", "model": "jev-1.13.0", "api_key": "test-jev"})
	var calls atomic.Int64
	s.client = fakeJev(t, &calls)
	root := makeSpan(t, "1111111111111111", "", map[string]any{"gen_ai.span.kind": "AGENT", "repomesh.subject_kind": "protocol_fixture", "repomesh.execution_purpose": "team", "repomesh.collection_status": "complete", "repomesh.execution_status": "completed", "repomesh.source_id": "test-runtime", "repomesh.expected_span_count": 2, "gen_ai.input.messages": "Read the contract"})
	tool := makeSpan(t, "2222222222222222", root.SpanID, map[string]any{"gen_ai.span.kind": "TOOL", "gen_ai.tool.name": "read_file", "repomesh.subject_kind": "protocol_fixture", "repomesh.execution_purpose": "team", "input.value": "contract.md", "output.value": "payable basis points"})
	if err := storeSpans(s.archives[0].Journal, []Span{root, tool}); err != nil {
		t.Fatal(err)
	}
	_, _ = request(t, h, "POST", "/api/bindings", map[string]any{"binding": TraceBinding{TraceID: root.TraceID, SourceID: "test-runtime", ProjectID: "p", IssueID: "i", TaskID: "t", AttemptID: "a"}, "evidence": map[string]string{"dispatch": "fixture"}})
	now := time.Now().UTC()
	s.onlineOnce(context.Background(), now)
	if calls.Load() != 0 {
		t.Fatal("default automatic billing")
	}
	status, _ := request(t, h, "PUT", "/api/evaluation-policy", EvaluationPolicy{Enabled: true, DailyCallLimit: 1, AllowFixtures: true, LateWindowSeconds: 0})
	if status != 200 {
		t.Fatal("policy failed")
	}
	s.onlineOnce(context.Background(), now)
	s.onlineOnce(context.Background(), now)
	if calls.Load() != 1 {
		t.Fatalf("expected one paid request, got %d", calls.Load())
	}
	restarted, err := New(Options{Archive: s.archives[0].Journal.Dir})
	if err != nil {
		t.Fatal(err)
	}
	restarted.client = s.client
	restarted.onlineOnce(context.Background(), now)
	if calls.Load() != 1 {
		t.Fatal("restart replayed a paid request")
	}
	root.TraceID = "22222222222222222222222222222222"
	tool.TraceID = root.TraceID
	root.Detail = json.RawMessage(`{"attributes":[{"key":"gen_ai.span.kind","value":{"stringValue":"AGENT"}},{"key":"repomesh.execution_purpose","value":{"stringValue":"judge"}},{"key":"repomesh.collection_status","value":{"stringValue":"complete"}},{"key":"repomesh.execution_status","value":{"stringValue":"completed"}}]}`)
	if err := storeSpans(s.archives[0].Journal, []Span{root, tool}); err != nil {
		t.Fatal(err)
	}
	restarted.onlineOnce(context.Background(), now.Add(24*time.Hour))
	if calls.Load() != 1 {
		t.Fatal("judge evaluation recursion")
	}
}

func TestDeepSeekInitialLabelsHaveNoScoresOrHumanAuthority(t *testing.T) {
	s, h := newTestServer(t)
	_, _ = request(t, h, "PUT", "/api/assistant", map[string]string{"model": "deepseek-flash", "api_key": "fake-ds"})
	_, _ = request(t, h, "POST", "/api/trials", map[string]string{"variant": "baseline"})
	rows, _ := observepipe.DatasetRows(s.archives[0].Journal)
	_, body := request(t, h, "POST", "/api/samples", map[string]string{"archive": s.archives[0].ID, "trial_id": rows[0].TrialID, "expected_revision": rows[0].EvidenceRefs[1], "reason": "verification_failed"})
	var sample Sample
	_ = json.Unmarshal(body, &sample)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer fake-ds" {
			t.Fatal("wrong assistant credential")
		}
		content, _ := json.Marshal(map[string]any{"category": "contract", "summary": "Frozen contract differs from consumed revision", "evidence_ids": []string{"sample:" + sample.ID}, "missing_evidence": []string{}})
		writeJSON(w, 200, map[string]any{"choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]string{"content": string(content)}}}, "usage": map[string]int{"prompt_tokens": 20, "completion_tokens": 10}})
	}))
	defer provider.Close()
	s.modelEndpoint = provider.URL
	s.client = provider.Client()
	status, body := request(t, h, "POST", "/api/analyses/attribution", map[string]string{"sample_id": sample.ID, "expected_revision": sample.SubjectRevision})
	if status != 201 {
		t.Fatalf("%d %s", status, body)
	}
	var analysis LocalAnalysis
	_ = json.Unmarshal(body, &analysis)
	if analysis.Status != "complete" {
		t.Fatalf("analysis failed: %s", body)
	}
	var label SampleAnnotation
	if s.archives[0].Journal.ReadRecord("annotations", analysis.ID, &label) != nil || label.Confirmed || label.ActorType != "ai" || label.Verdict != "unknown" {
		t.Fatal("AI annotation gained human/score authority")
	}
}

func TestPaidGradingReplayDoesNotRebillAfterResponseOrStorageFailure(t *testing.T) {
	for _, brokenStorage := range []bool{false, true} {
		t.Run(map[bool]string{false: "response_replay", true: "post_request_storage_failure"}[brokenStorage], func(t *testing.T) {
			s, h := newTestServer(t)
			_, _ = request(t, h, "PUT", "/api/model", map[string]string{"provider": "typesafe", "model": "jev-1.13.0", "api_key": "test-jev"})
			_, _ = request(t, h, "POST", "/api/trials", map[string]string{"variant": "candidate"})
			rows, _ := observepipe.DatasetRows(s.archives[0].Journal)
			var calls atomic.Int64
			s.client = fakeJev(t, &calls)
			if brokenStorage {
				if err := os.Symlink(t.TempDir(), filepath.Join(s.archives[0].Journal.Dir, "judgments")); err != nil {
					t.Fatal(err)
				}
			}
			payload := map[string]string{"archive": s.archives[0].ID, "trial_id": rows[0].TrialID, "attempt_key": "stable-http-attempt"}
			first, firstBody := request(t, h, "POST", "/api/judge", payload)
			second, secondBody := request(t, h, "POST", "/api/judge", payload)
			if calls.Load() != 1 {
				t.Fatalf("HTTP replay created %d paid calls", calls.Load())
			}
			if brokenStorage {
				if first < 400 || second != 409 {
					t.Fatal("unknown outcome not retained")
				}
			} else {
				var a, b Judgment
				_ = json.Unmarshal(firstBody, &a)
				_ = json.Unmarshal(secondBody, &b)
				if first != 201 || second != 201 || a.ID != b.ID || a.ClaimID == "" {
					t.Fatal("paid attempt/result link lost")
				}
			}
		})
	}
}

func TestModelProjectionRedactsNestedAndFreeTextCredentials(t *testing.T) {
	state := map[string]any{"tool_args": `{"password":"password-value","Authorization":"Bearer secondary-provider-token"}`, "message": "sk-another-provider-123456789 and developer@example.com", "key_in_prose": "literal-configured-key", "safe": "contract payable-ratio/v1"}
	clean := sanitizeModelState(state, "literal-configured-key")
	raw, _ := json.Marshal(clean)
	for _, secret := range []string{"password-value", "secondary-provider-token", "sk-another-provider-123456789", "developer@example.com", "literal-configured-key"} {
		if strings.Contains(string(raw), secret) {
			t.Fatalf("credential survived projection: %q", secret)
		}
	}
	if !strings.Contains(string(raw), "payable-ratio/v1") || !strings.Contains(string(raw), "removed_fields_or_patterns") {
		t.Fatal("projection lost task evidence or redaction status")
	}
}

func TestChildCompletenessAndUnspecifiedTTFTAreNotTrusted(t *testing.T) {
	s, _ := newTestServer(t)
	root := makeSpan(t, "1111111111111111", "", map[string]any{"gen_ai.span.kind": "AGENT", "gen_ai.input.messages": "task"})
	child := makeSpan(t, "2222222222222222", root.SpanID, map[string]any{"gen_ai.span.kind": "LLM", "gen_ai.response.time_to_first_token": int64(250000000), "repomesh.collection_status": "complete", "repomesh.execution_status": "completed", "repomesh.expected_span_count": 2})
	if err := storeSpans(s.archives[0].Journal, []Span{root, child}); err != nil {
		t.Fatal(err)
	}
	c, ok, err := s.projectCall(s.archives[0], child)
	if err != nil || !ok || c.TTFTMS != nil {
		t.Fatal("unknown TTFT units/first-token semantics accepted")
	}
	material, available, _, err := s.traceMaterial(&s.archives[0], root.TraceID)
	if err != nil || material["complete"] != false || available["tool_trajectory"] {
		t.Fatal("child span claimed whole-trajectory completeness")
	}
}

func TestConflictingPhysicalCallsAreExcludedFromMetrics(t *testing.T) {
	s, _ := newTestServer(t)
	a := makeSpan(t, "1111111111111111", "", map[string]any{"gen_ai.span.kind": "LLM", "repomesh.source_id": "source", "repomesh.call_id": "same", "gen_ai.usage.input_tokens": 100})
	b := makeSpan(t, "2222222222222222", "", map[string]any{"gen_ai.span.kind": "LLM", "repomesh.source_id": "source", "repomesh.call_id": "same", "gen_ai.usage.input_tokens": 500})
	if err := storeSpans(s.archives[0].Journal, []Span{a, b}); err != nil {
		t.Fatal(err)
	}
	calls, faults := s.calls()
	if len(calls) != 0 || len(faults) == 0 {
		t.Fatal("conflicting duplicate selected by iteration order")
	}
}

func TestLocalClusterThresholdIsEnforcedByCode(t *testing.T) {
	raw := json.RawMessage(`{"clusters":[{"name":"contract","sample_ids":["a","b"]},{"name":"migration","sample_ids":["c"]}],"outliers":["d"]}`)
	result, err := normalizeClusters(raw, map[string]bool{"a": true, "b": true, "c": true, "d": true})
	if err != nil {
		t.Fatal(err)
	}
	var output struct {
		Clusters []struct {
			IDs []string `json:"sample_ids"`
		} `json:"clusters"`
		Outliers []string `json:"outliers"`
	}
	_ = json.Unmarshal(result, &output)
	if len(output.Clusters) != 1 || len(output.Outliers) != 2 || output.Outliers[0] != "c" {
		t.Fatal("singleton theme presented as a valid cluster")
	}
	bad := json.RawMessage(`{"clusters":[{"name":"contract","sample_ids":["a","a"]}],"outliers":["c","invented"]}`)
	if _, err := normalizeClusters(bad, map[string]bool{"a": true, "b": true, "c": true, "d": true}); err == nil {
		t.Fatal("invented or duplicate membership accepted")
	}
}
