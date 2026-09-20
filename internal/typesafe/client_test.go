package typesafe

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"strings"
	"testing"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func responseFor(in Input) Response {
	r := Response{Model: DefaultModel, Answers: map[string]Answer{}}
	for _, c := range in.Claims {
		r.Answers[c.ID] = Answer{Type: "choice", Choice: "supported", Probabilities: map[string]float64{"supported": 0.9, "contradicted": 0.05, "insufficient": 0.05}, Confidence: 0.8}
	}
	r.Usage.InputTokens = 100
	r.Usage.OutputTokens = 20
	return r
}
func fakeResponse(req *http.Request) *http.Response {
	var wire struct {
		Questions map[string]json.RawMessage `json:"questions"`
	}
	_ = json.NewDecoder(req.Body).Decode(&wire)
	in := Input{}
	for id := range wire.Questions {
		in.Claims = append(in.Claims, Claim{ID: id})
	}
	data, _ := json.Marshal(responseFor(in))
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(data))), Header: make(http.Header)}
}
func sampleInput() Input {
	return Input{RequestID: "request-1", Claims: []Claim{{ID: "assertion", Text: "The test passed."}}, Evidence: "one assertion passed; exit 0"}
}

func TestClientUsesFixedTypedProtocolAndHidesUpstreamErrors(t *testing.T) {
	client := NewClient()
	client.http.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != "https://api.typesafe.ai/v1/systemone" || r.Header.Get("Authorization") != "Bearer fixture-key" {
			t.Fatal("wrong fixed endpoint or authentication")
		}
		var body map[string]json.RawMessage
		if json.NewDecoder(r.Body).Decode(&body) != nil || body["questions"] == nil || body["state"] == nil || body["model"] == nil || body["messages"] != nil {
			t.Fatal("wrong TypeSafe request contract")
		}
		encoded, _ := json.Marshal(responseFor(sampleInput()))
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(encoded))), Header: make(http.Header)}, nil
	})
	if _, err := client.Evaluate(context.Background(), []byte("fixture-key"), DefaultModel, sampleInput()); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		status int
		code   string
	}{{401, "TYPESAFE_KEY_REJECTED"}, {422, "TYPESAFE_REQUEST_REJECTED"}, {429, "TYPESAFE_RATE_LIMITED"}, {529, "TYPESAFE_OVERLOADED"}, {500, "TYPESAFE_UPSTREAM_FAILED"}, {302, "TYPESAFE_UPSTREAM_FAILED"}} {
		client.http.Transport = roundTrip(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: test.status, Body: io.NopCloser(strings.NewReader("fixture-key: raw private upstream diagnostic")), Header: make(http.Header)}, nil
		})
		_, err := client.Evaluate(context.Background(), []byte("fixture-key"), DefaultModel, sampleInput())
		if errorCode(err) != test.code || strings.Contains(err.Error(), "fixture-key") {
			t.Fatalf("HTTP %d did not produce a safe category", test.status)
		}
	}
	client.http.Transport = roundTrip(func(*http.Request) (*http.Response, error) { return nil, errors.New("private transport fixture-key") })
	if _, err := client.Evaluate(context.Background(), []byte("fixture-key"), DefaultModel, sampleInput()); errorCode(err) != "TYPESAFE_OUTCOME_UNKNOWN" {
		t.Fatal("ambiguous send must not become a safe-to-retry failure")
	}
}

func TestResponseValidationRejectsFabricatedOrIncompleteJudgments(t *testing.T) {
	for name, mutate := range map[string]func(*Response){
		"missing answer":       func(r *Response) { delete(r.Answers, "assertion") },
		"unknown outcome":      func(r *Response) { a := r.Answers["assertion"]; a.Choice = "approved"; r.Answers["assertion"] = a },
		"wrong primitive":      func(r *Response) { a := r.Answers["assertion"]; a.Type = "noul"; r.Answers["assertion"] = a },
		"invalid distribution": func(r *Response) { r.Answers["assertion"].Probabilities["supported"] = 0.2 },
		"wrong maximum":        func(r *Response) { a := r.Answers["assertion"]; a.Choice = "contradicted"; r.Answers["assertion"] = a },
		"NaN":                  func(r *Response) { a := r.Answers["assertion"]; a.Confidence = math.NaN(); r.Answers["assertion"] = a },
		"negative usage":       func(r *Response) { r.Usage.InputTokens = -1 },
	} {
		t.Run(name, func(t *testing.T) {
			r := responseFor(sampleInput())
			mutate(&r)
			if validResponse(r, sampleInput()) {
				t.Fatal("invalid response accepted")
			}
		})
	}
}

func TestInputLimitsAndCredentialRedaction(t *testing.T) {
	in := sampleInput()
	in.Evidence += " apikey_0123456789abcdef_0123456789abcdef Bearer abcdefghijklmnopqrstuvwxyz012345"
	normal, err := in.normalized()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(normal.Evidence, "apikey_") || strings.Contains(normal.Evidence, "Bearer") {
		t.Fatal("credential survived redaction")
	}
	in.Claims = append(in.Claims, in.Claims[0])
	if _, err = in.normalized(); err == nil {
		t.Fatal("duplicate claim ids accepted")
	}
	in = sampleInput()
	in.Evidence = strings.Repeat("x", 25<<10)
	if _, err = in.normalized(); err == nil {
		t.Fatal("oversized evidence accepted")
	}
}

func TestMissingResponseFieldsAreNotValidZeroValues(t *testing.T) {
	raw, _ := json.Marshal(responseFor(sampleInput()))
	if !completeWireResponse(raw) {
		t.Fatal("complete response rejected")
	}
	for _, field := range []string{"usage", "model"} {
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		delete(body, field)
		data, _ := json.Marshal(body)
		if completeWireResponse(data) {
			t.Fatalf("missing %s accepted", field)
		}
	}
	var body map[string]any
	_ = json.Unmarshal(raw, &body)
	answer := body["answers"].(map[string]any)["assertion"].(map[string]any)
	delete(answer, "confidence")
	data, _ := json.Marshal(body)
	if completeWireResponse(data) {
		t.Fatal("missing confidence accepted")
	}
}
