package scan

import (
	"context"
	"errors"
	"testing"
)

func scopeFixture() (CatalogStore, AliasRegistry) {
	cards := []RepositoryCard{
		// Selected: travel (depends on order, payment, common) and common.
		cardWithEvidence("t", "ts-travel", []string{"ts-travel"}, nil, []DepEvidence{
			{Name: "ts-order", Mechanism: MechanismRuntimeCall, Confidence: ConfidenceConfirmed},
			{Name: "ts-payment", Mechanism: MechanismDeploy, Confidence: ConfidenceDeclared},
			{Name: "ts-common", Mechanism: MechanismBuild, Confidence: ConfidenceConfirmed},
		}),
		cardWithEvidence("c", "ts-common", []string{"ts-common"}, nil, nil),
		// Outside the selection: order (missed dependency, confirmed),
		// payment (missed dependency, declared), notification (reverse
		// blast radius — it depends on the selected travel).
		cardWithEvidence("o", "ts-order", []string{"ts-order"}, nil, nil),
		cardWithEvidence("p", "ts-payment", []string{"ts-payment"}, nil, nil),
		cardWithEvidence("n", "ts-notification", nil, nil, []DepEvidence{
			{Name: "ts-travel", Mechanism: MechanismBuild, Confidence: ConfidenceConfirmed},
		}),
	}
	store := newMemoryStore()
	for _, card := range cards {
		store.rows[card.Name] = card
	}
	return store, BuildAliasRegistry(cards)
}

func TestCheckCompletenessReportsMissingAffectedAndInternalSilence(t *testing.T) {
	ctx := context.Background()
	store, registry := scopeFixture()

	report, err := CheckCompleteness(ctx, store, registry, []string{"t", "c"})
	if err != nil {
		t.Fatal(err)
	}

	if len(report.Missing) != 2 {
		t.Fatalf("missing = %+v, want order + payment", report.Missing)
	}
	byName := map[string]MissingDependency{}
	for _, entry := range report.Missing {
		byName[entry.Name] = entry
	}
	order, hasOrder := byName["ts-order"]
	payment, hasPayment := byName["ts-payment"]
	if !hasOrder || order.Confidence != ConfidenceConfirmed {
		t.Fatalf("ts-order missing entry = %+v (present=%v), want confirmed", order, hasOrder)
	}
	if !hasPayment || payment.Confidence != ConfidenceDeclared {
		t.Fatalf("ts-payment missing entry = %+v (present=%v), want declared", payment, hasPayment)
	}

	// The internal edge travel → common must NOT be reported as missing.
	if _, ok := byName["ts-common"]; ok {
		t.Fatal("a dependency inside the selection is not missing")
	}

	if len(report.Affected) != 1 {
		t.Fatalf("affected = %+v, want notification only", report.Affected)
	}
	if report.Affected[0].Name != "ts-notification" || report.Affected[0].Via[0] != "ts-travel" {
		t.Fatalf("affected entry = %+v", report.Affected[0])
	}
}

func TestCheckCompletenessRejectsUnknownRepositories(t *testing.T) {
	ctx := context.Background()
	store, registry := scopeFixture()

	_, err := CheckCompleteness(ctx, store, registry, []string{"t", "ghost"})
	var unknown *UnknownRepositoriesError
	if !errors.As(err, &unknown) {
		t.Fatalf("err = %v, want UnknownRepositoriesError", err)
	}
	if len(unknown.IDs) != 1 || unknown.IDs[0] != "ghost" {
		t.Fatalf("unknown ids = %v", unknown.IDs)
	}
}

func TestCheckCompletenessSurfacesAliasCollisions(t *testing.T) {
	ctx := context.Background()
	cards := []RepositoryCard{
		cardWithEvidence("a", "orders-alpha", []string{"orders"}, nil, nil),
		cardWithEvidence("b", "orders-beta", []string{"orders"}, nil, nil),
	}
	store := newMemoryStore()
	for _, card := range cards {
		store.rows[card.Name] = card
	}

	report, err := CheckCompleteness(ctx, store, BuildAliasRegistry(cards), []string{"a"})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Collisions) != 1 || report.Collisions[0] != "orders" {
		t.Fatalf("collisions = %v, want [orders]", report.Collisions)
	}
}

func TestNoSuggestionsIsManualMode(t *testing.T) {
	suggestions, err := NoSuggestions{}.Suggest("anything", 5)
	if err != nil || suggestions != nil {
		t.Fatalf("manual mode must never suggest: %v %v", suggestions, err)
	}
}

func TestSubmitScopeValidatesAtTheBoundary(t *testing.T) {
	ctx := context.Background()
	store, _ := scopeFixture()

	accepted := []string(nil)
	accept := func(ids []string) error { accepted = ids; return nil }

	if err := SubmitScope(ctx, store, []string{"t", "ghost"}, accept); err == nil {
		t.Fatal("unknown repository must reject the whole submission")
	}
	if accepted != nil {
		t.Fatal("accept must not run for a rejected scope")
	}

	if err := SubmitScope(ctx, store, []string{"t", "c"}, accept); err != nil {
		t.Fatalf("valid scope rejected: %v", err)
	}
	if len(accepted) != 2 {
		t.Fatalf("accepted = %v", accepted)
	}
}
