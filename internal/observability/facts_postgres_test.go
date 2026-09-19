package observability

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"repomesh.local/repomesh/internal/testdb"
)

func factSnapshot(t *testing.T, project, issue, state string) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(map[string]any{
		"schema_version": DiscoverySourceVersion, "event_kind": "discovery.state_saved",
		"project_id": project, "issue_id": issue, "state": state,
	})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func appendFact(t *testing.T, ctx context.Context, pool *pgxpool.Pool, project, issue string, snapshot json.RawMessage) {
	t.Helper()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if err := AppendDiscoveryFact(ctx, tx, project, issue, snapshot); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresFactsPreserveTransitionsAndExactBytes(t *testing.T) {
	pool := testdb.Open(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	f := testdb.SeedProject(t, pool, "", "", "facts/repository")
	const issue = "iss_fact_transitions"
	testdb.SeedIssue(t, pool, f, issue, "facts/repository")
	service := New(pool)
	baseline, err := service.ListFacts(ctx, f.ID, issue)
	if err != nil || len(baseline) != 0 {
		t.Fatalf("missing history must remain empty: %v, %v", baseline, err)
	}
	a := factSnapshot(t, f.ID, issue, "A")
	b := factSnapshot(t, f.ID, issue, "B")
	appendFact(t, ctx, pool, f.ID, issue, a)
	appendFact(t, ctx, pool, f.ID, issue, a)
	appendFact(t, ctx, pool, f.ID, issue, b)
	appendFact(t, ctx, pool, f.ID, issue, a)
	facts, err := service.ListFacts(ctx, f.ID, issue)
	if err != nil || len(facts) != 3 {
		t.Fatalf("A -> A -> B -> A should be three facts: %+v, %v", facts, err)
	}
	for i, want := range []json.RawMessage{a, b, a} {
		fact := facts[i]
		sum := sha256.Sum256(fact.Snapshot)
		if !bytes.Equal(fact.Snapshot, want) || fact.Fingerprint != hex.EncodeToString(sum[:]) {
			t.Fatalf("snapshot bytes/hash changed: %+v", fact)
		}
		if fact.ProjectID != f.ID || fact.IssueID != issue || fact.ID == "" || fact.OccurredAt.IsZero() || fact.SourceVersion != DiscoverySourceVersion {
			t.Fatalf("incomplete lineage: %+v", fact)
		}
	}
	if facts[0].ID == facts[2].ID {
		t.Fatal("returning to A reused an old event identity")
	}
	for _, query := range []string{
		`UPDATE repomesh_observability.facts SET source_version='changed' WHERE issue_id=$1`,
		`DELETE FROM repomesh_observability.facts WHERE issue_id=$1`,
	} {
		if _, err := pool.Exec(ctx, query, issue); err == nil {
			t.Fatal("immutable fact accepted a mutation")
		}
	}
	// The existing purge flag permits deletion only, never rewriting history.
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SET LOCAL repomesh.purge_mode='on'`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM repomesh_observability.facts WHERE issue_id=$1`, issue); err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	facts, err = service.ListFacts(ctx, f.ID, issue)
	if err != nil || len(facts) != 3 {
		t.Fatalf("rolled-back purge changed facts: %+v, %v", facts, err)
	}
}

func TestPostgresFactsLineageAndLateCommit(t *testing.T) {
	pool := testdb.Open(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	first := testdb.SeedProject(t, pool, "", "", "facts/first")
	second := testdb.SeedProject(t, pool, "", "", "facts/second")
	testdb.SeedIssue(t, pool, first, "iss_late", "facts/first")
	testdb.SeedIssue(t, pool, second, "iss_early", "facts/second")
	service := New(pool)
	for _, ids := range [][2]string{{"", "iss_late"}, {first.ID, ""}} {
		if _, err := service.ListFacts(ctx, ids[0], ids[1]); err == nil {
			t.Fatal("unbounded fact read accepted")
		}
	}
	if _, err := service.ListFacts(ctx, second.ID, "iss_late"); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("cross-project issue not rejected: %v", err)
	}
	late, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer late.Rollback(ctx)
	if err := AppendDiscoveryFact(ctx, late, first.ID, "iss_late", factSnapshot(t, first.ID, "iss_late", "late")); err != nil {
		t.Fatal(err)
	}
	appendFact(t, ctx, pool, second.ID, "iss_early", factSnapshot(t, second.ID, "iss_early", "early"))
	if facts, err := service.ListFacts(ctx, first.ID, "iss_late"); err != nil || len(facts) != 0 {
		t.Fatalf("uncommitted facts visible: %+v %v", facts, err)
	}
	early, err := service.ListFacts(ctx, second.ID, "iss_early")
	if err != nil || len(early) != 1 {
		t.Fatalf("committed independent issue missing: %+v %v", early, err)
	}
	if err := late.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	facts, err := service.ListFacts(ctx, first.ID, "iss_late")
	if err != nil || len(facts) != 1 || facts[0].ID == early[0].ID {
		t.Fatalf("late commit missing or misattributed: %+v %v", facts, err)
	}
	// Repeated collection is a read; it must return the same original fact.
	again, err := service.ListFacts(ctx, first.ID, "iss_late")
	if err != nil || len(again) != 1 || again[0].ID != facts[0].ID || !bytes.Equal(again[0].Snapshot, facts[0].Snapshot) {
		t.Fatalf("repeat read changed source evidence: %+v %v", again, err)
	}
	bad, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer bad.Rollback(ctx)
	if err := AppendDiscoveryFact(ctx, bad, second.ID, "iss_late", factSnapshot(t, second.ID, "iss_late", "wrong")); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("cross-project append accepted: %v", err)
	}
	if err := AppendDiscoveryFact(ctx, bad, first.ID, "iss_late", factSnapshot(t, second.ID, "iss_early", "wrong")); err == nil {
		t.Fatal("snapshot identity disagreed with fact identity")
	}
}
