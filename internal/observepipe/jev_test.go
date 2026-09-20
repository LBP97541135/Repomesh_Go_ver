package observepipe

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func typedExample(t *testing.T) ([]byte, map[string]JevQuestion) {
	t.Helper()
	q := map[string]JevQuestion{"quality": {Type: "score", Instructions: "Rate supported completeness", Criteria: []string{"none", "partial", "complete"}}, "support": {Type: "choice", Instructions: "Is evidence sufficient?", Criteria: map[string]string{"yes": "sufficient", "no": "insufficient"}}, "repeated": {Type: "noul", Instructions: "Is there a no-progress repeat?"}}
	b := []byte(`{"model":"jev-1.13.0","answers":{"quality":{"type":"score","score":1.75,"legend":{"0":"none","1":"partial","2":"complete"},"probabilities":{"0":0,"1":0.25,"2":0.75},"confidence":0.6},"support":{"type":"choice","choice":"yes","probabilities":{"yes":1,"no":0},"confidence":1},"repeated":{"type":"noul","noul":0}},"usage":{"input_tokens":100,"output_tokens":0}}`)
	return b, q
}

func TestJevTypedResponseRejectsMissingAndMismatchedEvidence(t *testing.T) {
	raw, q := typedExample(t)
	r, err := DecodeJevResponse(raw, q)
	if err != nil || *r.Answers["repeated"].Noul != 0 || r.Usage.OutputTokens != 0 {
		t.Fatalf("valid zero rejected: %v", err)
	}
	for name, mutate := range map[string]func(map[string]any){
		"missing_usage": func(v map[string]any) { delete(v["usage"].(map[string]any), "output_tokens") },
		"null_probability": func(v map[string]any) {
			v["answers"].(map[string]any)["support"].(map[string]any)["probabilities"].(map[string]any)["no"] = nil
		},
		"wrong_legend": func(v map[string]any) {
			v["answers"].(map[string]any)["quality"].(map[string]any)["legend"].(map[string]any)["2"] = "wrong criterion"
		},
		"wrong_expectation": func(v map[string]any) { v["answers"].(map[string]any)["quality"].(map[string]any)["score"] = 0.1 },
		"missing_noul":      func(v map[string]any) { delete(v["answers"].(map[string]any)["repeated"].(map[string]any), "noul") },
		"wrong_winner":      func(v map[string]any) { v["answers"].(map[string]any)["support"].(map[string]any)["choice"] = "no" },
		"extra_answer": func(v map[string]any) {
			v["answers"].(map[string]any)["invented"] = map[string]any{"type": "noul", "noul": 1}
		},
	} {
		t.Run(name, func(t *testing.T) {
			var v map[string]any
			_ = json.Unmarshal(raw, &v)
			mutate(v)
			b, _ := json.Marshal(v)
			if _, err := DecodeJevResponse(b, q); err == nil {
				t.Fatal("malformed result accepted")
			}
		})
	}
}

type jevTransport func(*http.Request) (*http.Response, error)

func (f jevTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestJevDoesNotFollowRedirectsOrReplayUnknownRequests(t *testing.T) {
	_, questions := typedExample(t)
	for _, status := range []int{302, 401, 422, 429, 529} {
		calls := 0
		client := &http.Client{Transport: jevTransport(func(r *http.Request) (*http.Response, error) {
			calls++
			if r.URL.String() != JevEndpoint || r.Header.Get("Authorization") != "Bearer test-key" {
				t.Fatal("wrong origin or credential")
			}
			return &http.Response{StatusCode: status, Header: http.Header{"Location": []string{"https://evil.invalid"}}, Body: io.NopCloser(strings.NewReader("test-key provider secret")), Request: r}, nil
		})}
		_, err := AskJev(context.Background(), client, "test-key", JevRequest{State: map[string]string{"input": "test"}, Model: "jev-1.13.0", Questions: questions})
		if err == nil || strings.Contains(err.Error(), "test-key") || calls != 1 {
			t.Fatalf("unsafe status %d: calls=%d err=%v", status, calls, err)
		}
	}
}

func TestJevRubricMissingTrajectoryDoesNotProduceEfficiencyScore(t *testing.T) {
	rubric := ObservationRubric()
	questions, grades := RubricQuestions(rubric, map[string]bool{"evidence": true})
	if _, ok := questions["tool_efficiency"]; ok {
		t.Fatal("paid grading attempted without trajectory")
	}
	var response JevResponse
	response.Answers = map[string]JevAnswer{}
	confidence, score := 0.99, 3.0
	response.Answers["evidence_quality"] = JevAnswer{Score: &score, Confidence: &confidence}
	response.Answers["evidence_quality_support"] = JevAnswer{Choice: "insufficient", Confidence: &confidence}
	result := ApplyRubric(rubric, response, grades)
	for _, g := range result {
		if g.Verdict != "unknown" || g.Score != nil {
			t.Fatal("insufficient evidence became a grade")
		}
	}
	v, _ := RubricSummary(result)
	if v != "unknown" {
		t.Fatal("unknown aggregate lost")
	}
}
