package scan

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestScanJobCreateRejectsUnknownFields(t *testing.T) {
	handler := &HTTP{}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest("POST", "/api/scan-jobs",
		strings.NewReader(`{"kind":"repository","url":"https://github.com/acme/orders","githubToken":"leak"}`))
	handler.handleScanJobCreate(recorder, request)
	if recorder.Code != 400 {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
	if body := recorder.Body.String(); !strings.Contains(body, "githubToken") {
		t.Fatalf("the offending field must be named: %s", body)
	}
}

type suggesterFunc func(requirement string, limit int) ([]Suggestion, error)

func (f suggesterFunc) Suggest(requirement string, limit int) ([]Suggestion, error) {
	return f(requirement, limit)
}

// Production assembles the adapter with a composite literal (main.go), so
// NewHTTP cannot be relied on: route registration itself must seed the boot
// default from the service config, and a later PUT must still override it.
func TestScopeAssistSeededFromServiceConfigAtRegistration(t *testing.T) {
	suggest := suggesterFunc(func(requirement string, limit int) ([]Suggestion, error) {
		return []Suggestion{{RepositoryID: "r1"}}, nil
	})
	newRequest := func() *http.Request {
		return httptest.NewRequest("POST", "/api/scope/suggestions",
			strings.NewReader(`{"requirement":"add a billing export"}`))
	}

	on := &HTTP{Service: &Service{cfg: Config{ScopeAssistEnabled: true}}, Suggester: suggest}
	on.RegisterRoutes(http.NewServeMux())
	recorder := httptest.NewRecorder()
	on.handleScopeSuggestions(recorder, newRequest())
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 with the config default on and no PUT: %s",
			recorder.Code, recorder.Body.String())
	}

	off := &HTTP{Service: &Service{}, Suggester: suggest}
	off.RegisterRoutes(http.NewServeMux())
	recorder = httptest.NewRecorder()
	off.handleScopeSuggestions(recorder, newRequest())
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 with the config default off", recorder.Code)
	}

	off.SetAssistEnabled(true)
	recorder = httptest.NewRecorder()
	off.handleScopeSuggestions(recorder, newRequest())
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 after the runtime override", recorder.Code)
	}
}

// The seam fires once per idempotency key: a replay returns the cached
// receipt without re-notifying, and a nil seam keeps the v1 behavior.
func TestScopeSubmitFiresDecisionSeamOncePerKey(t *testing.T) {
	store, _ := scopeFixture()
	var decisions []ScopeDecision
	handler := &HTTP{Store: store, OnScopeDecided: func(r *http.Request, d ScopeDecision) {
		decisions = append(decisions, d)
	}}
	body := `{"requirement":"add a billing export","repositoryIds":["t","c"],"idempotencyKey":"k1"}`

	recorder := httptest.NewRecorder()
	handler.handleScopeSubmit(recorder, httptest.NewRequest("POST", "/api/scope", strings.NewReader(body)))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", recorder.Code, recorder.Body.String())
	}
	if len(decisions) != 1 || decisions[0].Requirement != "add a billing export" ||
		decisions[0].IdempotencyKey != "k1" || !decisions[0].Accepted ||
		len(decisions[0].RepositoryIDs) != 2 {
		t.Fatalf("decisions = %+v", decisions)
	}

	recorder = httptest.NewRecorder()
	handler.handleScopeSubmit(recorder, httptest.NewRequest("POST", "/api/scope", strings.NewReader(body)))
	if recorder.Code != http.StatusOK {
		t.Fatalf("replay status = %d: %s", recorder.Code, recorder.Body.String())
	}
	if len(decisions) != 1 {
		t.Fatalf("replay fired the seam again (%d notifications)", len(decisions))
	}

	plain := &HTTP{Store: store}
	recorder = httptest.NewRecorder()
	plain.handleScopeSubmit(recorder, httptest.NewRequest("POST", "/api/scope",
		strings.NewReader(`{"repositoryIds":["t"],"idempotencyKey":"k2"}`)))
	if recorder.Code != http.StatusOK {
		t.Fatalf("nil seam status = %d: %s", recorder.Code, recorder.Body.String())
	}
}

// A panicking consumer must not fail the submission (F3 fail-open): the
// panic is contained, the receipt still caches, the caller sees 200.
func TestScopeSubmitSurvivesAPanickingSeam(t *testing.T) {
	store, _ := scopeFixture()
	handler := &HTTP{Store: store, OnScopeDecided: func(r *http.Request, d ScopeDecision) {
		panic("decision store exploded")
	}}
	recorder := httptest.NewRecorder()
	handler.handleScopeSubmit(recorder, httptest.NewRequest("POST", "/api/scope",
		strings.NewReader(`{"requirement":"x","repositoryIds":["t"],"idempotencyKey":"k9"}`)))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
}

type listStore struct {
	CatalogStore
	cards []RepositoryCard
}

func (s listStore) List(ctx context.Context) ([]RepositoryCard, error) {
	return s.cards, nil
}

func TestRepositoryDependentsEndpoint(t *testing.T) {
	handler := &HTTP{Store: listStore{cards: []RepositoryCard{
		{ID: "gw", Name: "gateway", AutoCard: &AutoCard{DepEvidence: []DepEvidence{
			{Name: "sdk", Mechanism: MechanismBuild, Confidence: ConfidenceConfirmed},
		}}},
		{ID: "sdk", Name: "sdk"},
	}}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/repositories/dependents", handler.handleRepositoryDependents)
	server := httptest.NewServer(mux)
	defer server.Close()

	resp, err := server.Client().Get(server.URL + "/api/repositories/dependents?name=sdk")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("dependents = %d, want 200", resp.StatusCode)
	}
	var body struct {
		Target     string `json:"target"`
		Dependents []struct {
			Repository string   `json:"repository"`
			Mechanisms []string `json:"mechanisms"`
			Confirmed  bool     `json:"confirmed"`
		} `json:"dependents"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Target != "sdk" || len(body.Dependents) != 1 ||
		body.Dependents[0].Repository != "gateway" || !body.Dependents[0].Confirmed {
		t.Fatalf("response = %+v", body)
	}

	if resp, err = server.Client().Get(server.URL + "/api/repositories/dependents"); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("missing name = %d, want 400", resp.StatusCode)
	}
	if resp, err = server.Client().Get(server.URL + "/api/repositories/dependents?name=ghost"); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown repo = %d, want 404", resp.StatusCode)
	}
}
