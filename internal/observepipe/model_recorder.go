package observepipe

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"repomesh.local/repomesh/internal/observability"
)

type RecordedModelCall struct {
	Schema   string                  `json:"schema"`
	SourceID string                  `json:"source_id"`
	Call     observability.ModelCall `json:"call"`
}

type ModelRecorder struct {
	j          *Journal
	source     string
	queue      chan observability.ModelCall
	mu         sync.RWMutex
	closed     bool
	done       chan struct{}
	accepted   atomic.Int64
	dropped    atomic.Int64
	written    atomic.Int64
	failed     atomic.Int64
	duplicates atomic.Int64
	onError    func(string)
}

// NewModelRecorder is composed by process entrypoints, never imported by a
// business service. Disk work happens off the model-request/transaction path.
func NewModelRecorder(dir, source string, onError func(string)) (*ModelRecorder, error) {
	if source == "" {
		return nil, errors.New("model observation source ID is required")
	}
	j, err := OpenJournal(dir)
	if err != nil {
		return nil, err
	}
	r := &ModelRecorder{j: j, source: source, queue: make(chan observability.ModelCall, 256), done: make(chan struct{}), onError: onError}
	go r.run()
	return r, nil
}

func (r *ModelRecorder) Observe(call observability.ModelCall) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.closed {
		r.dropped.Add(1)
		return
	}
	select {
	case r.queue <- call:
		r.accepted.Add(1)
	default:
		r.dropped.Add(1)
		if r.onError != nil {
			r.onError("observation queue full; model call not captured")
		}
	}
}

func (r *ModelRecorder) Close(ctx context.Context) error {
	r.mu.Lock()
	if !r.closed {
		r.closed = true
		close(r.queue)
	}
	r.mu.Unlock()
	select {
	case <-r.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (r *ModelRecorder) run() {
	defer close(r.done)
	for call := range r.queue {
		if call.ID == "" || call.StartedAt.IsZero() || call.DurationNS < 0 {
			r.failed.Add(1)
			continue
		}
		row := RecordedModelCall{Schema: "repomesh.model-call/1", SourceID: r.source, Call: call}
		var old RecordedModelCall
		readErr := r.j.ReadRecord("model_calls", r.source+":"+call.ID, &old)
		if readErr == nil {
			a, _ := json.Marshal(old)
			b, _ := json.Marshal(row)
			if string(a) == string(b) {
				r.duplicates.Add(1)
				r.persistStatus()
				continue
			}
		}
		if err := r.j.PutRecord("model_calls", r.source+":"+call.ID, row); err != nil {
			r.failed.Add(1)
			if r.onError != nil {
				r.onError("model observation persistence failed")
			}
		} else {
			r.written.Add(1)
		}
		r.persistStatus()
	}
	r.persistStatus()
}

func (r *ModelRecorder) persistStatus() {
	b, _ := json.Marshal(map[string]any{"source_id": r.source, "accepted": r.accepted.Load(), "persisted": r.written.Load(), "duplicate_observations": r.duplicates.Load(), "dropped": r.dropped.Load(), "failed": r.failed.Load(), "queue_limit": cap(r.queue), "checked_at": time.Now().UTC()})
	f, err := os.CreateTemp(r.j.Dir, ".capture-")
	if err != nil {
		if r.onError != nil {
			r.onError("capture health unavailable")
		}
		return
	}
	defer os.Remove(f.Name())
	_, err = f.Write(b)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(f.Name(), filepath.Join(r.j.Dir, "capture-"+Digest([]byte(r.source))+".json"))
	}
	if err != nil && r.onError != nil {
		r.onError("capture health persistence failed")
	}
}
