package observepipe_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"repomesh.local/repomesh/internal/discovery"
	"repomesh.local/repomesh/internal/observability"
	"repomesh.local/repomesh/internal/observepipe"
	"repomesh.local/repomesh/internal/testdb"
)

// This exercises a real use case and committed PostgreSQL history, without
// invoking an external model or touching a shared product database.
func TestPostgresDiscoveryToArchiveAndLocalOTLP(t *testing.T) {
	pool := testdb.Open(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	f := testdb.SeedProject(t, pool, "", "", "demo/price", "demo/order", "demo/unrelated")
	issue := "iss_observe_pipeline"
	testdb.SeedIssue(t, pool, f, issue, "demo/price", "demo/order", "demo/unrelated")
	svc := discovery.New(pool)
	if _, err := svc.Analysis(ctx, issue, "observer-contract-fixture", "analysis-1", nil, false); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Candidates(ctx, issue, "observer-contract-fixture", "candidates-1", 10, nil); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(t.TempDir(), "archive")
	// Optional evidence retention and real Collector export are explicit. The
	// integration run uses a fresh child directory, never existing evidence.
	if root := os.Getenv("REPOMESH_OBSERVE_TEST_OUTPUT"); root != "" {
		dir = filepath.Join(root, time.Now().UTC().Format("20060102T150405.000000000"))
	}
	j, err := observepipe.OpenJournal(dir)
	if err != nil {
		t.Fatal(err)
	}
	source := observability.New(pool)
	first, err := observepipe.CollectDiscoveryFrom(ctx, source, j, "isolated-postgres-test", f.ID, issue)
	if err != nil || first.Added != 2 {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	repeated, err := observepipe.CollectDiscoveryFrom(ctx, source, j, "isolated-postgres-test", f.ID, issue)
	if err != nil || repeated.Existing != 2 || repeated.Added != 0 {
		t.Fatalf("repeat=%+v err=%v", repeated, err)
	}
	events, err := j.Events()
	if err != nil || len(events) != 2 {
		t.Fatal(events, err)
	}
	for _, e := range events {
		var snapshot json.RawMessage
		if err = j.ReadEvidence(e.EvidenceRefs[0], &snapshot); err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(snapshot, []byte(issue)) || e.ProjectID != f.ID {
			t.Fatal("wrong issue lineage")
		}
	}
	if os.Getenv("REPOMESH_OBSERVE_TEST_OTLP_ENDPOINT") != "" {
		c := observepipe.ExportConfig{DestinationID: "real-local-collector-db-acceptance", EndpointEnv: "REPOMESH_OBSERVE_TEST_OTLP_ENDPOINT", ServiceName: "repomesh-db-acceptance", TimeoutSeconds: 5}
		result, err := observepipe.ExportOTLP(ctx, j, c)
		if err != nil || result.Exported != 2 {
			t.Fatalf("real collector=%+v err=%v", result, err)
		}
	}
	t.Logf("real PostgreSQL discovery archive: %s; project=%s issue=%s events=%d", dir, f.ID, issue, len(events))
}
