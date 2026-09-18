package scan

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"repomesh.local/repomesh/internal/testdb"
)

func TestPostgresCatalogRoundTrip(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()
	store := NewPostgresCatalog(pool)

	suffix := randSuffix()
	card := RepositoryCard{
		ID:           "card-" + suffix,
		Name:         "integration-" + suffix,
		URL:          "https://example.com/" + suffix,
		Description:  "operator typed this",
		TestCommands: []string{"pytest"},
		ScanStatus:   ScanStatusOK,
	}
	if err := store.Add(ctx, card); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(ctx,
			`DELETE FROM repomesh_scan.repositories WHERE id = $1`, card.ID); err != nil {
			t.Errorf("cleanup failed: %v", err)
		}
	})

	// No card yet (hand-registered): metadata empty.
	stored, err := store.Get(ctx, card.ID)
	if err != nil || stored == nil {
		t.Fatalf("row missing after add: %v", err)
	}
	if stored.AutoCard != nil {
		t.Fatal("hand-registered row must have no card")
	}

	// Refresh #1: card lands, operator fields survive.
	if err := store.UpdateAutoCard(ctx, card.ID,
		AutoCard{Deps: []string{"v1"}, DepEvidence: []DepEvidence{
			{Name: "org.services:ts-common", Mechanism: MechanismBuild, Confidence: ConfidenceConfirmed},
		}}, []string{"java"}, "sha-1"); err != nil {
		t.Fatal(err)
	}
	stored, err = store.Get(ctx, card.ID)
	if err != nil || stored == nil || stored.AutoCard == nil || len(stored.AutoCard.Deps) != 1 {
		t.Fatalf("card not stored: %v", err)
	}
	if stored.AutoCard.DepEvidence[0].Mechanism != MechanismBuild {
		t.Fatal("evidence mechanism lost in round trip")
	}
	if stored.Description != "operator typed this" || stored.TestCommands[0] != "pytest" || len(stored.Languages) != 1 || stored.Languages[0] != "java" {
		t.Fatal("refresh must not touch operator-owned fields")
	}

	// Refresh #2: whole replacement, idempotent.
	if err := store.UpdateAutoCard(ctx, card.ID, AutoCard{Deps: []string{"v2"}}, []string{"java"}, "sha-2"); err != nil {
		t.Fatal(err)
	}
	stored, _ = store.Get(ctx, card.ID)
	if stored.AutoCard.Deps[0] != "v2" || stored.Fingerprint != "sha-2" {
		t.Fatal("second refresh did not replace the card whole")
	}

	// Observed block: lands on the row, survives a card refresh (a scan
	// refresh must never wipe runtime evidence), missing id reports cleanly.
	if err := store.ReplaceObservedCalls(ctx, card.ID, []ObservedCall{
		{Target: "payment", Calls: 120},
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateAutoCard(ctx, card.ID, AutoCard{Deps: []string{"v3"}}, nil, "sha-3"); err != nil {
		t.Fatal(err)
	}
	stored, _ = store.Get(ctx, card.ID)
	if stored == nil || len(stored.ObservedCalls) != 1 || stored.ObservedCalls[0].Target != "payment" {
		t.Fatalf("observed block lost across refresh: %+v", stored)
	}
	if stored.AutoCard.Deps[0] != "v3" {
		t.Fatalf("card refresh overwrote by the observed import: %+v", stored.AutoCard)
	}
	if err := store.ReplaceObservedCalls(ctx, "no-such-id", nil); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("missing-id observed replace = %v, want ErrNoRows", err)
	}

	// By-name lookup and missing id.
	byName, err := store.GetByName(ctx, card.Name)
	if err != nil || byName == nil || byName.ID != card.ID {
		t.Fatalf("GetByName failed: %v", err)
	}
	if err := store.UpdateAutoCard(ctx, "no-such-id", AutoCard{}, nil, ""); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("missing-id update = %v, want ErrNoRows", err)
	}
}

func randSuffix() string {
	raw := make([]byte, 8)
	if _, err := rand.Read(raw); err != nil {
		panic(err)
	}
	return hex.EncodeToString(raw)
}
