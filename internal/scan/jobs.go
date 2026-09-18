package scan

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// ScanJob is the API view of one background scan (design doc §3.D-3/4):
// 202 + polling. Jobs are in-process records — a process restart loses
// them, and polling a lost job answers 404 (the honest semantics the
// Python version shipped).
type ScanJob struct {
	ID                    string `json:"id"`
	Kind                  string `json:"kind"` // organization | repository
	URL                   string `json:"url"`
	Status                string `json:"status"` // running | succeeded | failed
	Total                 int    `json:"total"`
	Scanned               int    `json:"scanned"`
	LastScannedRepository string `json:"lastScannedRepository,omitempty"`
	Registered            int    `json:"registered"`
	Skipped               int    `json:"skipped"`
	Failed                int    `json:"failed"`
	Error                 string `json:"error,omitempty"`
	StartedAt             string `json:"startedAt"`
	FinishedAt            string `json:"finishedAt,omitempty"`
}

// ScanJobStatus values.
const (
	JobRunning   = "running"
	JobSucceeded = "succeeded"
	JobFailed    = "failed"
)

// JobRegistry keeps in-process scan jobs and runs them in the background.
// A non-nil mirror persists snapshots so ids survive restarts.
type JobRegistry struct {
	mu     sync.Mutex
	seq    int
	jobs   map[string]*ScanJob
	mirror *JobMirror
}

// NewJobRegistry returns an empty registry.
func NewJobRegistry() *JobRegistry {
	return &JobRegistry{jobs: map[string]*ScanJob{}}
}

// WithMirror attaches a persistence mirror.
func (r *JobRegistry) WithMirror(mirror *JobMirror) *JobRegistry {
	r.mirror = mirror
	return r
}

// Start launches run in a background goroutine (fresh context: a scan
// outlives the HTTP request that started it) and returns the job id.
// progress receives (done, total, repository name) while running.
func (r *JobRegistry) Start(kind, url string, run func(ctx context.Context, progress func(done, total int, name string)) (RegistrationCounts, error)) string {
	r.mu.Lock()
	// One running job per (kind, url): a duplicate submit joins the
	// existing job instead of racing itself for the same catalog rows.
	for _, job := range r.jobs {
		if job.Status == JobRunning && job.Kind == kind && job.URL == url {
			r.mu.Unlock()
			return job.ID
		}
	}
	r.seq++
	id := fmt.Sprintf("scan-%d-%d", time.Now().Unix(), r.seq)
	job := &ScanJob{
		ID:        id,
		Kind:      kind,
		URL:       url,
		Status:    JobRunning,
		StartedAt: time.Now().UTC().Format(time.RFC3339),
	}
	r.jobs[id] = job
	r.mu.Unlock()

	r.mirror.Save(context.Background(), *job)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		counts, err := run(ctx, func(done, total int, name string) {
			r.mu.Lock()
			job.Scanned = done
			if total > job.Total {
				job.Total = total
			}
			job.LastScannedRepository = name
			snapshot := *job
			r.mu.Unlock()
			r.mirror.Save(context.Background(), snapshot)
		})
		r.mu.Lock()
		defer r.mu.Unlock()
		if err != nil {
			job.Status = JobFailed
			job.Error = err.Error()
		} else {
			job.Status = JobSucceeded
		}
		job.Total = counts.Total
		job.Scanned = counts.Total
		job.Registered = counts.Registered
		job.Skipped = counts.Skipped
		job.Failed = counts.Failed
		now := time.Now().UTC().Format(time.RFC3339)
		job.FinishedAt = now
		r.mirror.Save(context.Background(), *job)
	}()
	return id
}

// Get returns a snapshot of one job; unknown in-process ids fall back to
// the persistence mirror so a restart does not orphan the polling client.
func (r *JobRegistry) Get(id string) (ScanJob, bool) {
	r.mu.Lock()
	job, ok := r.jobs[id]
	r.mu.Unlock()
	if ok {
		return *job, true
	}
	return r.mirror.Load(context.Background(), id)
}
