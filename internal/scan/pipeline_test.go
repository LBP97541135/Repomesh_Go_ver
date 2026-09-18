package scan

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"repomesh.local/repomesh/internal/reposcan"
)

// ---------------------------------------------------------------------------
// fakes
// ---------------------------------------------------------------------------

type fakeFetcher struct {
	heads     map[string]string
	trees     map[string][]reposcan.TreeEntry
	contents  map[string]map[string]string
	repos     []reposcan.RepoInfo
	failTrees map[string]bool

	treeCalls int
}

func (f *fakeFetcher) FetchTree(ctx context.Context, repoURL string) ([]reposcan.TreeEntry, error) {
	f.treeCalls++
	if f.failTrees[repoURL] {
		return nil, errors.New("tree unavailable")
	}
	return f.trees[repoURL], nil
}

func (f *fakeFetcher) FetchCommits(ctx context.Context, repoURL string, limit int) ([]string, error) {
	return []string{"feat: something concrete"}, nil
}

func (f *fakeFetcher) FetchFileContent(ctx context.Context, repoURL string, path string) (string, error) {
	return f.contents[repoURL][path], nil
}

func (f *fakeFetcher) FetchHead(ctx context.Context, repoURL string) (string, error) {
	return f.heads[repoURL], nil
}

func (f *fakeFetcher) ResolveName(ctx context.Context, repoURL string) (string, error) {
	segments := strings.Split(strings.TrimRight(repoURL, "/"), "/")
	return segments[len(segments)-1], nil
}

func (f *fakeFetcher) ListRepos(ctx context.Context, groupURL string) ([]reposcan.RepoInfo, error) {
	return f.repos, nil
}

// pipelineStore is an in-memory CatalogStore that mirrors the real refresh
// semantics: UpdateAutoCard replaces scan-derived data and never touches
// operator-owned fields.
type pipelineStore struct {
	rows map[string]RepositoryCard
}

func newFakeStore() *pipelineStore { return &pipelineStore{rows: map[string]RepositoryCard{}} }

func (s *pipelineStore) Add(ctx context.Context, card RepositoryCard) error {
	if _, exists := s.rows[card.Name]; exists {
		return errors.New("duplicate")
	}
	s.rows[card.Name] = card
	return nil
}

func (s *pipelineStore) List(ctx context.Context) ([]RepositoryCard, error) {
	out := make([]RepositoryCard, 0, len(s.rows))
	for _, row := range s.rows {
		out = append(out, row)
	}
	return out, nil
}

func (s *pipelineStore) Get(ctx context.Context, id string) (*RepositoryCard, error) {
	for _, row := range s.rows {
		if row.ID == id {
			row := row
			return &row, nil
		}
	}
	return nil, nil
}

func (s *pipelineStore) GetByName(ctx context.Context, name string) (*RepositoryCard, error) {
	if row, ok := s.rows[name]; ok {
		row := row
		return &row, nil
	}
	return nil, nil
}

// ReplaceObservedCalls implements CatalogStore: the observed block rides on
// the row and survives card refreshes.
func (s *pipelineStore) ReplaceObservedCalls(ctx context.Context, id string, calls []ObservedCall) error {
	for name, row := range s.rows {
		if row.ID == id {
			row.ObservedCalls = calls
			s.rows[name] = row
			return nil
		}
	}
	return errors.New("missing row")
}

func (s *pipelineStore) UpdateAutoCard(ctx context.Context, id string, card AutoCard, languages []string, fingerprint string) error {
	for name, row := range s.rows {
		if row.ID == id {
			// Operator-owned fields (description, topics, test commands/paths)
			// survive a refresh untouched.
			row.AutoCard = &card
			row.Languages = languages
			row.Fingerprint = fingerprint
			row.ProfiledAt = "refreshed"
			s.rows[name] = row
			return nil
		}
	}
	return errors.New("missing row")
}

// ---------------------------------------------------------------------------
// fixtures
// ---------------------------------------------------------------------------

const (
	repoOrders = "https://github.com/acme/orders"
	ordersPom  = `<?xml version="1.0"?><project><groupId>com.demo</groupId><artifactId>orders</artifactId>
<dependencies><dependency><groupId>org.services</groupId><artifactId>ts-common</artifactId></dependency></dependencies></project>`
	ordersFeign = "@FeignClient(name = \"ts-payment-service\")\npublic interface OrderClient {}"
	ordersYML   = "spring:\n  datasource:\n    url: jdbc:mysql://orders-db:3306/orders\n"
)

func ordersTree() []reposcan.TreeEntry {
	return []reposcan.TreeEntry{
		{Path: "pom.xml"},
		{Path: "src", IsDir: true},
		{Path: "src/main/java/com/demo/OrderClient.java"},
		{Path: "src/main/resources/application.yml"},
	}
}

func newTestRunner(fetcher *fakeFetcher, store *pipelineStore) *Runner {
	return &Runner{
		Fetcher:    fetcher,
		Channels:   []EvidenceChannel{NewBuildChannel(), NewRuntimeCallChannel(), NewResourceChannel()},
		Store:      store,
		MaxWorkers: 2,
	}
}

// ---------------------------------------------------------------------------
// tests
// ---------------------------------------------------------------------------

func TestScanSingleRegistersNewRepository(t *testing.T) {
	fetcher := &fakeFetcher{
		heads: map[string]string{repoOrders: "sha-a1"},
		trees: map[string][]reposcan.TreeEntry{repoOrders: ordersTree()},
		contents: map[string]map[string]string{repoOrders: {
			"pom.xml": ordersPom, "src/main/java/com/demo/OrderClient.java": ordersFeign,
			"src/main/resources/application.yml": ordersYML,
		}},
	}
	store := newFakeStore()
	runner := newTestRunner(fetcher, store)

	counts, err := runner.ScanSingle(context.Background(), repoOrders)
	if err != nil {
		t.Fatal(err)
	}
	if counts != (RegistrationCounts{Total: 1, Registered: 1}) {
		t.Fatalf("counts = %+v", counts)
	}

	card, err := store.GetByName(context.Background(), "orders")
	if err != nil || card == nil {
		t.Fatalf("orders not registered: %v", err)
	}
	if card.Fingerprint != "sha-a1" {
		t.Fatalf("fingerprint = %q", card.Fingerprint)
	}
	if fmt.Sprint(card.Languages) != "[java]" {
		t.Fatalf("languages = %v (derived from build manifests)", card.Languages)
	}
	// Three channels contributed: BUILD dep, RUNTIME_CALL dep, shared resource.
	if len(card.AutoCard.Deps) != 2 || len(card.AutoCard.DepEvidence) != 3 {
		t.Fatalf("card = %+v", card.AutoCard)
	}
}

func TestIncrementalGateSkipsUnchangedRepository(t *testing.T) {
	fetcher := &fakeFetcher{
		heads:    map[string]string{repoOrders: "sha-a1"},
		trees:    map[string][]reposcan.TreeEntry{repoOrders: ordersTree()},
		contents: map[string]map[string]string{repoOrders: {"pom.xml": ordersPom}},
	}
	store := newFakeStore()
	runner := newTestRunner(fetcher, store)
	if _, err := runner.ScanSingle(context.Background(), repoOrders); err != nil {
		t.Fatal(err)
	}
	treeCallsAfterFirstScan := fetcher.treeCalls

	counts, err := runner.ScanSingle(context.Background(), repoOrders)
	if err != nil {
		t.Fatal(err)
	}
	if counts.Skipped != 1 || counts.Registered != 0 {
		t.Fatalf("unchanged re-scan counts = %+v", counts)
	}
	if fetcher.treeCalls != treeCallsAfterFirstScan {
		t.Fatalf("the gate must run before the tree is fetched: %d -> %d",
			treeCallsAfterFirstScan, fetcher.treeCalls)
	}
}

func TestRescanWithChangedHeadRefreshesCard(t *testing.T) {
	fetcher := &fakeFetcher{
		heads:    map[string]string{repoOrders: "sha-a1"},
		trees:    map[string][]reposcan.TreeEntry{repoOrders: ordersTree()},
		contents: map[string]map[string]string{repoOrders: {"pom.xml": ordersPom}},
	}
	store := newFakeStore()
	runner := newTestRunner(fetcher, store)
	if _, err := runner.ScanSingle(context.Background(), repoOrders); err != nil {
		t.Fatal(err)
	}

	// The operator stamps the row; a refresh must keep it.
	stored := store.rows["orders"]
	stored.Description = "operator wrote this"
	stored.TestCommands = []string{"pytest"}
	store.rows["orders"] = stored
	fetcher.heads[repoOrders] = "sha-a2"

	counts, err := runner.ScanSingle(context.Background(), repoOrders)
	if err != nil {
		t.Fatal(err)
	}
	if counts.Registered != 1 || counts.Skipped != 0 {
		t.Fatalf("changed head must refresh: %+v", counts)
	}
	card := store.rows["orders"]
	if card.Fingerprint != "sha-a2" {
		t.Fatalf("fingerprint = %q (counts=%+v)", card.Fingerprint, counts)
	}
	if card.Description != "operator wrote this" || fmt.Sprint(card.TestCommands) != "[pytest]" {
		t.Fatalf("operator fields must survive a refresh: %+v", card)
	}
}

func TestScanOrganizationFailureSemantics(t *testing.T) {
	repoBroken := "https://github.com/acme/broken"
	fetcher := &fakeFetcher{
		heads: map[string]string{
			repoOrders: "sha-a1",
			repoBroken: "sha-b1",
		},
		trees: map[string][]reposcan.TreeEntry{
			repoOrders: ordersTree(),
		},
		contents: map[string]map[string]string{repoOrders: {"pom.xml": ordersPom}},
		repos: []reposcan.RepoInfo{
			{Name: "orders", URL: repoOrders},
			{Name: "broken", URL: repoBroken},
			{Name: "archived-fork", URL: "https://github.com/acme/old", Archived: true, Fork: true},
		},
		failTrees: map[string]bool{repoBroken: true},
	}
	store := newFakeStore()
	runner := newTestRunner(fetcher, store)

	counts, err := runner.ScanOrganization(context.Background(), "https://github.com/acme")
	if err != nil {
		t.Fatal(err)
	}

	// The archived fork is filtered before counting; the broken repository
	// is counted as failed and must not be in the catalog.
	if counts.Total != 2 || counts.Registered != 1 || counts.Failed != 1 {
		t.Fatalf("counts = %+v", counts)
	}
	if _, exists := store.rows["broken"]; exists {
		t.Fatal("a failed scan must never enter the catalog")
	}
	card, exists := store.rows["orders"]
	if !exists || card.AutoCard == nil || len(card.AutoCard.Deps) == 0 {
		t.Fatalf("orders card missing or empty: %+v %+v", card, exists)
	}
}

func TestOrganizationGateSkipsUnchanged(t *testing.T) {
	fetcher := &fakeFetcher{
		heads:    map[string]string{repoOrders: "sha-a1"},
		trees:    map[string][]reposcan.TreeEntry{repoOrders: ordersTree()},
		contents: map[string]map[string]string{repoOrders: {"pom.xml": ordersPom}},
		repos: []reposcan.RepoInfo{
			{Name: "orders", URL: repoOrders},
		},
	}
	store := newFakeStore()
	runner := newTestRunner(fetcher, store)

	if _, err := runner.ScanOrganization(context.Background(), "https://github.com/acme"); err != nil {
		t.Fatal(err)
	}
	treeCallsAfterFirstScan := fetcher.treeCalls

	counts, err := runner.ScanOrganization(context.Background(), "https://github.com/acme")
	if err != nil {
		t.Fatal(err)
	}
	if counts.Skipped != 1 || counts.Total != 1 {
		t.Fatalf("organization gate counts = %+v", counts)
	}
	if fetcher.treeCalls != treeCallsAfterFirstScan {
		t.Fatal("an unchanged repository must not re-fetch its tree")
	}
}
