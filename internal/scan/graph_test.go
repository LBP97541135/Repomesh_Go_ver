package scan

import "testing"

func cardWithEvidence(id, name string, identities, deployIdentities []string, evidence []DepEvidence) RepositoryCard {
	return RepositoryCard{
		ID:   id,
		Name: name,
		AutoCard: &AutoCard{
			Identities:       identities,
			DeployIdentities: deployIdentities,
			DepEvidence:      evidence,
		},
		ScanStatus: ScanStatusOK,
	}
}

func TestAliasRegistryAuthorityWins(t *testing.T) {
	cards := []RepositoryCard{
		// A's platform name collides with B's self-declared identity.
		cardWithEvidence("a", "ts-order", nil, nil, nil),
		cardWithEvidence("b", "order-backend", []string{"ts-order"}, nil, nil),
	}
	registry := BuildAliasRegistry(cards)

	if owner, ok := registry.Resolve("TS-ORDER"); !ok || owner.ID != "a" {
		t.Fatalf("authoritative name must win the collision, got %v ok=%v", owner, ok)
	}
	if owner, ok := registry.Resolve("order-backend"); !ok || owner.ID != "b" {
		t.Fatalf("unclaimed alias must resolve to its declarer, got %v ok=%v", owner, ok)
	}
	if _, ok := registry.Resolve("no-such-thing"); ok {
		t.Fatal("unknown identifier must not resolve")
	}
}

func TestBuildGraphResolvesEdgesAndSkipsTheRest(t *testing.T) {
	cards := []RepositoryCard{
		// travel declares identity "ts-travel" and depends on:
		//   - "ts-order"        → another scanned repo  (edge)
		//   - "spring-boot"     → unresolvable library  (no edge)
		//   - "ts-travel"       → itself                (no edge)
		cardWithEvidence("travel", "ts-travel", []string{"ts-travel"}, nil, []DepEvidence{
			{Name: "ts-order", Mechanism: MechanismRuntimeCall, Confidence: ConfidenceConfirmed},
			{Name: "spring-boot", Mechanism: MechanismBuild, Confidence: ConfidenceConfirmed},
			{Name: "ts-travel", Mechanism: MechanismBuild, Confidence: ConfidenceConfirmed},
			// same target again via a second mechanism: kept as its own edge
			{Name: "ts-order", Mechanism: MechanismBuild, Confidence: ConfidenceConfirmed},
			// duplicate of the first (same target + mechanism): dropped
			{Name: "ts-order", Mechanism: MechanismRuntimeCall, Confidence: ConfidenceConfirmed},
		}),
		cardWithEvidence("order", "ts-order", []string{"ts-order"}, nil, nil),
	}

	graph := BuildGraph(cards, BuildAliasRegistry(cards))
	if len(graph.Edges) != 2 {
		t.Fatalf("edges = %+v, want 2 (runtime_call + build to order)", graph.Edges)
	}
	for _, edge := range graph.Edges {
		if edge.FromID != "travel" || edge.ToID != "order" {
			t.Fatalf("unexpected edge %+v", edge)
		}
	}
}

func TestOrderUsesConfirmedEdgesOnlyAndReportsCycles(t *testing.T) {
	// payment depends on order depends on common; the DECLARED edge
	// (notification → payment) is a hint and must not affect sequencing.
	edges := []Edge{
		{FromID: "payment", ToID: "order", Mechanism: MechanismBuild, Confidence: ConfidenceConfirmed},
		{FromID: "order", ToID: "common", Mechanism: MechanismBuild, Confidence: ConfidenceConfirmed},
		{FromID: "notification", ToID: "payment", Mechanism: MechanismDeploy, Confidence: ConfidenceDeclared},
	}
	cards := []RepositoryCard{
		cardWithEvidence("common", "common", nil, nil, nil),
		cardWithEvidence("order", "order", nil, nil, nil),
		cardWithEvidence("payment", "payment", nil, nil, nil),
		cardWithEvidence("notification", "notification", nil, nil, nil),
	}
	graph := Graph{Edges: edges}
	ordered, cyclic := graph.Order(cards)

	joined := map[string]int{}
	for i, id := range ordered {
		joined[id] = i
	}
	if len(cyclic) != 0 || len(ordered) != 4 {
		t.Fatalf("ordered=%v cyclic=%v", ordered, cyclic)
	}
	if !(joined["common"] < joined["order"] && joined["order"] < joined["payment"]) {
		t.Fatalf("dependency order violated: %v", ordered)
	}

	// common ↔ order on confirmed edges is a cycle: both come out unordered.
	cycled := Graph{Edges: []Edge{
		{FromID: "common", ToID: "order", Mechanism: MechanismBuild, Confidence: ConfidenceConfirmed},
		{FromID: "order", ToID: "common", Mechanism: MechanismBuild, Confidence: ConfidenceConfirmed},
	}}
	ordered, cyclic = cycled.Order(cards)
	if len(ordered) != 2 || len(cyclic) != 2 {
		t.Fatalf("cycle not isolated: ordered=%v cyclic=%v", ordered, cyclic)
	}
}

func TestAliasRegistryReportsCollisions(t *testing.T) {
	cards := []RepositoryCard{
		// a, b, c all self-declare the identity "orders" — a three-way alias
		// collision. d's authoritative platform name IS "orders": it wins the
		// resolution no matter what the aliases say.
		cardWithEvidence("a", "orders-alpha", []string{"orders"}, nil, nil),
		cardWithEvidence("b", "orders-beta", []string{"orders"}, nil, nil),
		cardWithEvidence("c", "gamma", []string{"orders"}, nil, nil),
		cardWithEvidence("d", "orders", nil, nil, nil),
	}
	registry := BuildAliasRegistry(cards)

	if owner, _ := registry.Resolve("orders"); owner.ID != "d" {
		t.Fatalf("authoritative name must win, went to %s", owner.ID)
	}
	if len(registry.Collisions) != 1 || registry.Collisions[0] != "orders" {
		t.Fatalf("collisions = %v, want exactly [orders]", registry.Collisions)
	}
}
