package scan

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// The #13 acceptance scenario: two repositories with a static declared
// dependency, one repository observed but never declared (a discovered
// edge), and one declared dependency that never fired (a dead-dependency
// hint). Unmapped APM services are reported, never guessed.
func TestImportObserved(t *testing.T) {
	store := newFakeStore()
	must := func(card RepositoryCard) {
		if err := store.Add(context.Background(), card); err != nil {
			t.Fatal(err)
		}
	}
	must(RepositoryCard{
		ID: "orders", Name: "orders", URL: "https://git.example/orders",
		AutoCard: &AutoCard{DepEvidence: []DepEvidence{
			{Name: "payment", Mechanism: MechanismRuntimeCall, Confidence: ConfidenceConfirmed},
			{Name: "inventory", Mechanism: MechanismBuild, Confidence: ConfidenceConfirmed},
		}},
	})
	must(RepositoryCard{ID: "payment", Name: "payment", URL: "https://git.example/payment"})
	must(RepositoryCard{ID: "inventory", Name: "inventory", URL: "https://git.example/inventory"})

	registry := BuildAliasRegistry(mustCards(t, store))
	topology := ObservedTopology{
		Source:      "skywalking",
		GeneratedAt: "2026-09-15T10:00:00Z",
		Services:    []string{"orders", "payment", "inventory", "mystery-gateway"},
		Calls: []ObservedTopologyCall{
			{Source: "orders", Target: "payment", Calls: 120},
			{Source: "mystery-gateway", Target: "orders", Calls: 7},
		},
	}

	report, err := ImportObserved(context.Background(), store, registry, topology)
	if err != nil {
		t.Fatal(err)
	}

	if report.MappedCalls != 1 {
		t.Fatalf("mapped calls = %d, want 1", report.MappedCalls)
	}
	if fmt.Sprint(report.UpdatedRepositories) != "[orders]" {
		t.Fatalf("updated = %v", report.UpdatedRepositories)
	}
	if fmt.Sprint(report.UnmappedServices) != "[mystery-gateway]" {
		t.Fatalf("unmapped = %v", report.UnmappedServices)
	}
	// orders -> payment: declared AND observed → validated.
	validated := strings.Join(report.ValidatedEdges, "|")
	if !strings.Contains(validated, "orders -> payment (120 calls)") {
		t.Fatalf("validated edges = %v", report.ValidatedEdges)
	}
	// orders -> inventory: declared, both endpoints observed, never fired
	// → dead-dependency hint.
	if fmt.Sprint(report.ZeroCallHints) != "[orders -> inventory]" {
		t.Fatalf("zero-call hints = %v", report.ZeroCallHints)
	}

	// The observed block landed on the caller's card as OBSERVED evidence.
	orders := store.rows["orders"]
	if len(orders.ObservedCalls) != 1 || orders.ObservedCalls[0].Target != "payment" ||
		orders.ObservedCalls[0].Calls != 120 {
		t.Fatalf("observed calls = %+v", orders.ObservedCalls)
	}

	// The graph now carries the OBSERVED edge with its traffic weight.
	graph := BuildGraph(mustCards(t, store), registry)
	found := false
	for _, edge := range graph.Edges {
		if edge.Mechanism == MechanismObserved {
			found = true
			if edge.FromID != "orders" || edge.ToID != "payment" || edge.Calls != 120 {
				t.Fatalf("observed edge = %+v", edge)
			}
		}
	}
	if !found {
		t.Fatal("observed edge missing from the graph")
	}
}

func TestImportObservedIdempotent(t *testing.T) {
	store := newFakeStore()
	mustAdd(t, store, RepositoryCard{ID: "orders", Name: "orders", URL: "https://git.example/orders"})
	registry := BuildAliasRegistry(mustCards(t, store))
	topology := ObservedTopology{
		Services: []string{"orders"},
		Calls:    []ObservedTopologyCall{{Source: "orders", Target: "orders", Calls: 5}},
	}
	// Self calls contribute nothing; re-import must not accumulate.
	for range 3 {
		report, err := ImportObserved(context.Background(), store, registry, topology)
		if err != nil {
			t.Fatal(err)
		}
		if report.MappedCalls != 0 {
			t.Fatalf("self call mapped: %+v", report)
		}
	}
	orders := store.rows["orders"]
	if len(orders.ObservedCalls) != 0 {
		t.Fatalf("self calls must not land: %+v", orders.ObservedCalls)
	}
}

func mustCards(t *testing.T, store *pipelineStore) []RepositoryCard {
	t.Helper()
	cards, err := store.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return cards
}

func mustAdd(t *testing.T, store *pipelineStore, card RepositoryCard) {
	t.Helper()
	if err := store.Add(context.Background(), card); err != nil {
		t.Fatal(err)
	}
}
