package observepipe

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"repomesh.local/repomesh/internal/observability"
)

func journalEvent(t *testing.T, j *Journal, key string) Event {
	t.Helper()
	ref, err := j.PutJSON(map[string]string{"input": "中文\nprivate-input-not-for-OTLP"})
	if err != nil {
		t.Fatal(err)
	}
	e := NewEvent("test-producer", "test/1", key, "test.committed", "work-1", time.Date(2026, 9, 19, 1, 2, 3, 0, time.UTC))
	e.ContextManifestRef = ref
	e.EvidenceRefs = []string{ref}
	e.CollectionStatus = "complete"
	return e
}

func TestJournalConcurrentIdentityAndIntegrity(t *testing.T) {
	j, err := OpenJournal(filepath.Join(t.TempDir(), "archive"))
	if err != nil {
		t.Fatal(err)
	}
	e := journalEvent(t, j, "event-1")
	var wg sync.WaitGroup
	results := make(chan Event, 12)
	errs := make(chan error, 12)
	for range 12 {
		wg.Go(func() {
			copy := journalEvent(t, j, "event-1")
			saved, _, err := j.Append(copy)
			results <- saved
			errs <- err
		})
	}
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var tid string
	for saved := range results {
		if tid != "" && tid != saved.TraceID {
			t.Fatal("concurrent writers created multiple identities")
		}
		tid = saved.TraceID
	}
	events, err := j.Events()
	if err != nil || len(events) != 1 {
		t.Fatalf("events=%d err=%v", len(events), err)
	}
	e.OutcomeVerdict = "fail"
	if _, _, err = j.Append(e); err == nil {
		t.Fatal("same source changed meaning")
	}
	if err = os.WriteFile(filepath.Join(j.Dir, events[0].ContextManifestRef), []byte(`{"input":"tampered"}`), 0600); err != nil {
		t.Fatal(err)
	}
	var content any
	if j.ReadEvidence(events[0].ContextManifestRef, &content) == nil {
		t.Fatal("corruption undetected")
	}
}

func TestJournalRejectsMissingEvidenceUnknownSchemaAndSymlink(t *testing.T) {
	j, _ := OpenJournal(filepath.Join(t.TempDir(), "archive"))
	e := journalEvent(t, j, "event-1")
	e.SchemaVersion = "repomesh-observe/99"
	if _, _, err := j.Append(e); err == nil {
		t.Fatal("unknown schema accepted")
	}
	e.SchemaVersion = EventSchema
	e.ContextManifestRef = "../../secret"
	if _, _, err := j.Append(e); err == nil {
		t.Fatal("escaping reference accepted")
	}
	other := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(other, link); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenJournal(link); err == nil {
		t.Fatal("symlink archive accepted")
	}
	if err := os.Symlink(other, filepath.Join(j.Dir, "events")); err != nil {
		t.Fatal(err)
	}
	e = journalEvent(t, j, "event-2")
	if _, _, err := j.Append(e); err == nil {
		t.Fatal("symlink bucket accepted")
	}
}

type memoryFacts struct{ rows []observability.Fact }

func (m *memoryFacts) ListFacts(context.Context, string, string) ([]observability.Fact, error) {
	return m.rows, nil
}

func TestCollectRescansStableScopeWithoutDroppingLateFacts(t *testing.T) {
	build := func(id string) observability.Fact {
		raw := json.RawMessage(`{"schema_version":"repomesh-discovery/0.1","project_id":"p","issue_id":"i"}`)
		return observability.Fact{ID: id, ProjectID: "p", IssueID: "i", OccurredAt: time.Date(2026, 9, 19, 1, 0, 0, 0, time.UTC), SourceVersion: observability.DiscoverySourceVersion, Snapshot: raw, Fingerprint: Digest(raw)}
	}
	j, _ := OpenJournal(filepath.Join(t.TempDir(), "archive"))
	source := &memoryFacts{[]observability.Fact{build("2")}}
	first, err := CollectDiscoveryFrom(t.Context(), source, j, "db-A", "p", "i")
	if err != nil || first.Added != 1 {
		t.Fatal(first, err)
	}
	source.rows = append(source.rows, build("1"))
	next, err := CollectDiscoveryFrom(t.Context(), source, j, "db-A", "p", "i")
	if err != nil || next.Added != 1 || next.Existing != 1 {
		t.Fatal(next, err)
	}
	other, err := CollectDiscoveryFrom(t.Context(), source, j, "db-B", "p", "i")
	if err != nil || other.Added != 2 {
		t.Fatal("source DB collision", other, err)
	}
	source.rows[0].SourceVersion = "unknown/3"
	if _, err = CollectDiscoveryFrom(t.Context(), source, j, "db-A", "p", "i"); err == nil {
		t.Fatal("unknown source accepted")
	}
}
