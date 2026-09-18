package decisionchain

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// fakeStore is the in-memory Store for HTTP-level tests.
type fakeStore struct {
	mu         sync.Mutex
	nodes      map[string]DecisionNode
	byEvent    map[string]string
	versions   map[string]int
	embeddings map[string]string // nodeID -> model
	features   map[string]bool
	structural []DecisionNode
	semantic   []ScoredNode
	upserts    int
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		nodes:      map[string]DecisionNode{},
		byEvent:    map[string]string{},
		versions:   map[string]int{},
		embeddings: map[string]string{},
		features:   map[string]bool{},
	}
}

func (f *fakeStore) Record(ctx context.Context, w nodeWrite) (DecisionNode, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if id, ok := f.byEvent[w.EventID]; ok {
		return f.nodes[id], nil
	}
	key := w.RequirementKey + "|" + string(w.Step)
	f.versions[key]++
	node := DecisionNode{
		ID: NewUUIDv4(), EventID: w.EventID,
		RequirementText: w.RequirementText, RequirementKey: w.RequirementKey,
		Step: w.Step, Version: f.versions[key], Status: w.Status,
		ActorType: "human", ActorID: w.ActorID, Action: w.Action,
		Rationale: w.Rationale, ContextRef: w.ContextRef,
		AffectedRepositories: w.AffectedRepositories, Source: w.Source,
		CreatedAt: time.Now(),
	}
	f.nodes[node.ID] = node
	f.byEvent[w.EventID] = node.ID
	return node, nil
}

func (f *fakeStore) List(ctx context.Context, filter Filter) ([]DecisionNode, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []DecisionNode{}
	for _, node := range f.nodes {
		if filter.Repository != "" && !containsName(node.AffectedRepositories, filter.Repository) {
			continue
		}
		out = append(out, node)
	}
	return out, nil
}

func (f *fakeStore) Get(ctx context.Context, id string) (*DecisionNode, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if node, ok := f.nodes[id]; ok {
		return &node, nil
	}
	return nil, pgx.ErrNoRows
}

func (f *fakeStore) PendingEmbeddings(ctx context.Context, model string, limit int) ([]DecisionNode, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []DecisionNode{}
	for _, node := range f.nodes {
		if got := f.embeddings[node.ID]; got == "" || got != model {
			out = append(out, node)
		}
	}
	return out, nil
}

func (f *fakeStore) UpsertEmbedding(ctx context.Context, nodeID, model string, vec []float32, at time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.embeddings[nodeID] = model
	f.upserts++
	return nil
}

func (f *fakeStore) SemanticCandidates(ctx context.Context, model string, query []float32, limit int) ([]ScoredNode, error) {
	return f.semantic, nil
}

func (f *fakeStore) StructuralCandidates(ctx context.Context, names []string, limit int) ([]DecisionNode, error) {
	return f.structural, nil
}

func (f *fakeStore) FeatureEnabled(ctx context.Context, feature string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	enabled, ok := f.features[feature]
	if !ok {
		return true, nil
	}
	return enabled, nil
}

func (f *fakeStore) SetFeature(ctx context.Context, feature string, enabled bool, by string, at time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.features[feature] = enabled
	return nil
}

func containsName(names []string, want string) bool {
	for _, name := range names {
		if name == want {
			return true
		}
	}
	return false
}

func testNode(id, eventID string, repos ...string) DecisionNode {
	return DecisionNode{
		ID: id, EventID: eventID, RequirementText: "为项目选择合适的仓库",
		RequirementKey: RequirementKey("为项目选择合适的仓库"),
		Step:           StepConfirmation, Version: 1, Status: StatusConfirmed,
		ActorType: "human", ActorID: "xiaochen", Action: "范围圈定确认",
		Rationale:            "为项目选择合适的仓库",
		ContextRef:           map[string]any{},
		AffectedRepositories: repos, Source: SourceEvent, CreatedAt: time.Now(),
	}
}

func testService(store Store, cfg Config) *Service {
	if cfg.EmbeddingModel == "" {
		cfg.EmbeddingModel = "bge-m3"
	}
	return &Service{cfg: cfg, store: store, client: &http.Client{}}
}

func testServer(svc *Service) *httptest.Server {
	mux := http.NewServeMux()
	svc.RegisterRoutes(mux)
	return httptest.NewServer(mux)
}

func putJSON(client *http.Client, url, body string) int {
	req, err := http.NewRequest(http.MethodPut, url, strings.NewReader(body))
	if err != nil {
		panic(err)
	}
	resp, err := client.Do(req)
	if err != nil {
		panic(err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

func TestSettingsToggleGatesDecisionEndpoints(t *testing.T) {
	store := newFakeStore()
	svc := testService(store, Config{})
	server := testServer(svc)
	defer server.Close()

	client := server.Client()
	get := func(path string) int {
		resp, err := client.Get(server.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}

	store.features["decision_chain"] = false
	if got := get("/api/decision-chains"); got != http.StatusServiceUnavailable {
		t.Fatalf("list while off = %d, want 503", got)
	}
	if got := get("/api/decision-chains/similar?requirement=x"); got != http.StatusServiceUnavailable {
		t.Fatalf("similar while off = %d, want 503", got)
	}
	if got := get("/api/settings/decision-chain"); got != http.StatusOK {
		t.Fatalf("settings GET while off = %d, want 200", got)
	}

	if got := putJSON(server.Client(), server.URL+"/api/settings/decision-chain", `{"enabled": true}`); got != http.StatusOK {
		t.Fatalf("settings PUT = %d, want 200", got)
	}
	if got := get("/api/decision-chains"); got != http.StatusOK {
		t.Fatalf("list after enabling = %d, want 200", got)
	}
}

func TestPutSettingRejectsUnknownFieldsAndMissingValue(t *testing.T) {
	store := newFakeStore()
	server := testServer(testService(store, Config{}))
	defer server.Close()

	if got := putJSON(server.Client(), server.URL+"/api/settings/decision-chain", `{"enabled": true, "extra": 1}`); got != http.StatusBadRequest {
		t.Fatalf("unknown field = %d, want 400", got)
	}
	if got := putJSON(server.Client(), server.URL+"/api/settings/decision-chain", `{}`); got != http.StatusBadRequest {
		t.Fatalf("missing enabled = %d, want 400", got)
	}
}

func TestListAndGetRoutes(t *testing.T) {
	store := newFakeStore()
	node := testNode("node-1", "evt-1", "repo-a")
	store.nodes[node.ID] = node
	server := testServer(testService(store, Config{}))
	defer server.Close()

	resp, err := server.Client().Get(server.URL + "/api/decision-chains")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list = %d, want 200", resp.StatusCode)
	}

	resp, err = server.Client().Get(server.URL + "/api/decision-chains/node-1")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get = %d, want 200", resp.StatusCode)
	}
	resp, err = server.Client().Get(server.URL + "/api/decision-chains/missing")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("missing get = %d, want 404", resp.StatusCode)
	}
}

func TestSimilarRequiresRequirementAndLabelsStructural(t *testing.T) {
	store := newFakeStore()
	store.structural = []DecisionNode{testNode("node-1", "evt-1", "repo-a", "repo-b")}
	server := testServer(testService(store, Config{}))
	defer server.Close()

	resp, err := server.Client().Get(server.URL + "/api/decision-chains/similar")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("similar without requirement = %d, want 400", resp.StatusCode)
	}

	resp, err = server.Client().Get(server.URL + "/api/decision-chains/similar?requirement=选择仓库&repositoryIds=repo-a,repo-c")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("similar = %d, want 200", resp.StatusCode)
	}
	body := mustDecode(t, resp)
	if body["mode"] != "structural" {
		t.Fatalf("mode = %v, want structural", body["mode"])
	}
	hits := body["hits"].([]any)
	if len(hits) != 1 {
		t.Fatalf("hits = %d, want 1", len(hits))
	}
	hit := hits[0].(map[string]any)
	// |{repo-a} ∩ {repo-a, repo-b}| / |{repo-a, repo-c} ∪ {repo-a, repo-b}| = 1/3
	if hit["score"].(float64) < 0.32 || hit["score"].(float64) > 0.34 {
		t.Fatalf("jaccard score = %v, want ~0.333", hit["score"])
	}
	if hit["matchedRepositories"].([]any)[0] != "repo-a" {
		t.Fatalf("matched = %v, want repo-a", hit["matchedRepositories"])
	}
}

func TestSemanticSearchStatusCodes(t *testing.T) {
	store := newFakeStore()
	node := testNode("node-1", "evt-1", "repo-a")
	store.semantic = []ScoredNode{{Node: node, Score: 0.9}}

	// Unconfigured: 503.
	unconfigured := testServer(testService(store, Config{}))
	defer unconfigured.Close()
	resp, err := unconfigured.Client().Get(unconfigured.URL + "/api/decision-chains/semantic-search?queryText=x")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("unconfigured = %d, want 503", resp.StatusCode)
	}

	// Provider failure: 502 (no silent degrade on the explicit probe).
	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer failing.Close()
	failingSvc := testService(store, Config{EmbeddingBaseURL: failing.URL})
	failingServer := testServer(failingSvc)
	defer failingServer.Close()
	resp, err = failingServer.Client().Get(failingServer.URL + "/api/decision-chains/semantic-search?queryText=x")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("provider failure = %d, want 502", resp.StatusCode)
	}

	// Healthy provider: 200 with the preset hit.
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		vectors := make([]float64, embeddingDims)
		writeJSON(w, http.StatusOK, map[string]any{
			"data": []map[string]any{{"index": 0, "embedding": vectors}},
		})
	}))
	defer good.Close()
	goodSvc := testService(store, Config{EmbeddingBaseURL: good.URL})
	goodServer := testServer(goodSvc)
	defer goodServer.Close()
	resp, err = goodServer.Client().Get(goodServer.URL + "/api/decision-chains/semantic-search?queryText=x")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("healthy = %d, want 200", resp.StatusCode)
	}
	body := mustDecode(t, resp)
	hits := body["hits"].([]any)
	if len(hits) != 1 || body["mode"] != "semantic" {
		t.Fatalf("semantic hits = %v mode = %v, want 1 hit / semantic", body["hits"], body["mode"])
	}
}

func TestRefreshEndpointCounts(t *testing.T) {
	store := newFakeStore()
	node := testNode("node-1", "evt-1", "repo-a")
	store.nodes[node.ID] = node

	dims := embeddingDims
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		vectors := make([]float64, dims)
		writeJSON(w, http.StatusOK, map[string]any{
			"data": []map[string]any{{"index": 0, "embedding": vectors}},
		})
	}))
	defer good.Close()
	svc := testService(store, Config{EmbeddingBaseURL: good.URL})
	server := testServer(svc)
	defer server.Close()

	resp, err := server.Client().Post(server.URL+"/api/decision-chains/embeddings/refresh", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("refresh = %d, want 200", resp.StatusCode)
	}
	body := mustDecode(t, resp)
	if body["refreshed"].(float64) != 1 || body["failed"].(float64) != 0 {
		t.Fatalf("refresh result = %v, want refreshed 1 failed 0", body)
	}

	// Wrong dimension: honest failure count with a reason. A fresh node is
	// pending again (the first node now carries a current-model vector).
	node2 := testNode("node-2", "evt-2", "repo-b")
	store.nodes[node2.ID] = node2
	dims = 3
	resp, err = server.Client().Post(server.URL+"/api/decision-chains/embeddings/refresh", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body = mustDecode(t, resp)
	if body["refreshed"].(float64) != 0 || body["failed"].(float64) != 1 {
		t.Fatalf("wrong-dims refresh = %v, want refreshed 0 failed 1", body)
	}
	if detail, _ := body["reason"].(string); !strings.Contains(detail, "dimension") {
		t.Fatalf("reason = %q, want dimension mismatch", detail)
	}

	// Unconfigured: honest zeros.
	plain := testServer(testService(store, Config{}))
	defer plain.Close()
	resp, err = plain.Client().Post(plain.URL+"/api/decision-chains/embeddings/refresh", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body = mustDecode(t, resp)
	if body["refreshed"].(float64) != 0 || body["failed"].(float64) != 0 {
		t.Fatalf("unconfigured refresh = %v, want zeros", body)
	}
}

func mustDecode(t *testing.T, resp *http.Response) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return body
}
