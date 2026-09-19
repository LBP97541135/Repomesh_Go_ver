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
	Status                string `json:"status"` // running | succeeded | partial | failed
	Total                 int    `json:"total"`
	Scanned               int    `json:"scanned"`
	LastScannedRepository string `json:"lastScannedRepository,omitempty"`
	Registered            int    `json:"registered"`
	Skipped               int    `json:"skipped"`
	Failed                int    `json:"failed"`
	// RateLimited 是"因平台限流而根本没尝试"的仓库数（见 RegistrationCounts）。
	RateLimited           int    `json:"rateLimited,omitempty"`
	Error                 string `json:"error,omitempty"`
	// TokenSource 如实说明这次扫描**用的是谁的凭据**：
	//   user       = 发起人自己的 GitHub 令牌（5000 次/小时，按账号隔离）
	//   deployment = 部署级只读令牌（环境变量配置的兜底）
	//   anonymous  = 无凭据（60 次/小时，只够扫几个仓库）
	// 用户看到"6 成功 40 失败"时，这一栏能直接回答"为什么会这样"。
	TokenSource           string `json:"tokenSource,omitempty"`
	StartedAt             string `json:"startedAt"`
	FinishedAt            string `json:"finishedAt,omitempty"`
}

// ScanJobStatus values.
const (
	JobRunning   = "running"
	JobSucceeded = "succeeded"
	// JobPartial 表示扫描跑完了、但**有仓库没能登记**（限流、读不到、写失败…）。
	// 2026-09-19 事故：46 个仓库里 40 个因 GitHub 限流失败，任务级却报 succeeded
	// ——"整体成功"与"40 个失败"同时成立，等于对用户说谎。有失败就必须说成
	// partial，并在 error 里写清失败数与原因。
	JobPartial = "partial"
	JobFailed  = "failed"
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
	return r.StartWithTokenSource(kind, url, "", run)
}

// StartWithTokenSource is Start plus the credential-provenance label that the
// UI shows next to the counts.
func (r *JobRegistry) StartWithTokenSource(kind, url, tokenSource string, run func(ctx context.Context, progress func(done, total int, name string)) (RegistrationCounts, error)) string {
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
		ID:          id,
		Kind:        kind,
		URL:         url,
		Status:      JobRunning,
		TokenSource: tokenSource,
		StartedAt:   time.Now().UTC().Format(time.RFC3339),
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
		} else if counts.AbortedReason != "" || counts.Failed > 0 {
			job.Status = JobPartial
			job.RateLimited = counts.RateLimited
			if counts.AbortedReason != "" {
				// 限流中止的原因比"N 个失败"更具体，优先如实呈现。
				job.Error = counts.AbortedReason
			} else {
				job.Error = fmt.Sprintf("%d/%d 个仓库未能登记（多为平台限流或读不到，可稍后重扫）", counts.Failed, counts.Total)
			}
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
