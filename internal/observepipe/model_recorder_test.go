package observepipe

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"repomesh.local/repomesh/internal/observability"
)

func TestReadOnlyArchiveNeverCreatesOrMutates(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "archive")
	if _, err := OpenJournalReadOnly(dir); err == nil {
		t.Fatal("missing read archive accepted")
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("read opener created a directory")
	}
	j, err := OpenJournal(dir)
	if err != nil {
		t.Fatal(err)
	}
	ref, err := j.PutJSON(map[string]string{"fact": "original"})
	if err != nil {
		t.Fatal(err)
	}
	ro, err := OpenJournalReadOnly(dir)
	if err != nil {
		t.Fatal(err)
	}
	var body any
	if ro.ReadEvidence(ref, &body) != nil {
		t.Fatal("read unavailable")
	}
	if _, err := ro.PutJSON(map[string]string{"fact": "new"}); err == nil {
		t.Fatal("read-only evidence write allowed")
	}
	if err := ro.PutRecord("samples", "id", map[string]string{"id": "id"}); err == nil {
		t.Fatal("read-only record write allowed")
	}
}

func TestModelRecorderDrainsAndPreservesMissingUsage(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "archive")
	r, err := NewModelRecorder(dir, "deployment/web", nil)
	if err != nil {
		t.Fatal(err)
	}
	call := observability.ModelCall{ID: "physical-1", ProjectID: "p", IssueID: "i", RequestedModel: "test-model", StartedAt: time.Now().UTC(), DurationNS: 123456789, Status: "error"}
	r.Observe(call)
	r.Observe(call)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := r.Close(ctx); err != nil {
		t.Fatal(err)
	}
	var saved RecordedModelCall
	if err := r.j.ReadRecord("model_calls", "deployment/web:physical-1", &saved); err != nil {
		t.Fatal(err)
	}
	if saved.Call.InputTokens != nil || saved.Call.OutputTokens != nil || saved.Call.DurationNS != 123456789 {
		t.Fatal("invented usage or timing")
	}
	rows, err := r.j.Records("model_calls")
	if err != nil || len(rows) != 1 {
		t.Fatal("duplicate physical request counted twice")
	}
	if r.written.Load() != 1 || r.duplicates.Load() != 1 {
		t.Fatal("health counters double-counted an idempotent replay")
	}
}

func TestModelCaptureCallerOverheadIsBounded(t *testing.T) {
	r, err := NewModelRecorder(filepath.Join(t.TempDir(), "archive"), "perf-local", nil)
	if err != nil {
		t.Fatal(err)
	}
	off, on := []time.Duration{}, []time.Duration{}
	for i := 0; i < 35; i++ {
		start := time.Now()
		time.Sleep(2 * time.Millisecond)
		baseline := time.Since(start)
		start = time.Now()
		time.Sleep(2 * time.Millisecond)
		r.Observe(observability.ModelCall{ID: fmt.Sprint(i), StartedAt: start.UTC(), DurationNS: time.Since(start).Nanoseconds(), Status: "ok"})
		observed := time.Since(start)
		if i >= 5 {
			off = append(off, baseline)
			on = append(on, observed)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := r.Close(ctx); err != nil {
		t.Fatal(err)
	}
	sort.Slice(off, func(i, j int) bool { return off[i] < off[j] })
	sort.Slice(on, func(i, j int) bool { return on[i] < on[j] })
	extra := on[15] - off[15]
	limit := max(5*time.Millisecond, off[15]/20)
	if extra > limit || r.dropped.Load() != 0 || r.failed.Load() != 0 || r.written.Load() != 35 {
		t.Fatalf("capture overhead=%v limit=%v written=%d failed=%d dropped=%d", extra, limit, r.written.Load(), r.failed.Load(), r.dropped.Load())
	}
	t.Logf("local caller median baseline=%v observed=%v extra=%v; 30 measured pairs; physical model/cloud latency not measured", off[15], on[15], extra)
}
