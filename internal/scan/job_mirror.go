package scan

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// JobMirror persists scan job state to repomesh_scan.scan_jobs so a job id
// stays queryable after a restart. The in-process registry stays the
// authority for live progress; the mirror is a read-through fallback.
type JobMirror struct {
	pool *pgxpool.Pool
}

// NewJobMirror builds the mirror; a nil pool disables persistence.
func NewJobMirror(pool *pgxpool.Pool) *JobMirror {
	if pool == nil {
		return nil
	}
	return &JobMirror{pool: pool}
}

// Save upserts one job snapshot.
func (m *JobMirror) Save(ctx context.Context, job ScanJob) {
	if m == nil {
		return
	}
	var finishedAt *time.Time
	if job.FinishedAt != "" {
		if parsed, err := time.Parse(time.RFC3339, job.FinishedAt); err == nil {
			finishedAt = &parsed
		}
	}
	startedAt := time.Now().UTC()
	if parsed, err := time.Parse(time.RFC3339, job.StartedAt); err == nil {
		startedAt = parsed
	}
	_, _ = m.pool.Exec(ctx, `INSERT INTO repomesh_scan.scan_jobs
		(id, kind, url, status, total, scanned, last_scanned_repository, registered, skipped, failed, error, started_at, finished_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
		ON CONFLICT (id) DO UPDATE SET status=EXCLUDED.status, total=EXCLUDED.total, scanned=EXCLUDED.scanned,
		  last_scanned_repository=EXCLUDED.last_scanned_repository, registered=EXCLUDED.registered,
		  skipped=EXCLUDED.skipped, failed=EXCLUDED.failed, error=EXCLUDED.error, finished_at=EXCLUDED.finished_at`,
		job.ID, job.Kind, job.URL, job.Status, job.Total, job.Scanned, job.LastScannedRepository,
		job.Registered, job.Skipped, job.Failed, job.Error, startedAt, finishedAt)
}

// Load reads one job from the mirror; ok=false when unknown.
func (m *JobMirror) Load(ctx context.Context, id string) (ScanJob, bool) {
	if m == nil {
		return ScanJob{}, false
	}
	var job ScanJob
	var finishedAt *time.Time
	var startedAt time.Time
	err := m.pool.QueryRow(ctx, `SELECT id, kind, url, status, total, scanned, last_scanned_repository,
		registered, skipped, failed, error, started_at, finished_at
		FROM repomesh_scan.scan_jobs WHERE id=$1`, id).
		Scan(&job.ID, &job.Kind, &job.URL, &job.Status, &job.Total, &job.Scanned, &job.LastScannedRepository,
			&job.Registered, &job.Skipped, &job.Failed, &job.Error, &startedAt, &finishedAt)
	if err != nil {
		return ScanJob{}, false
	}
	job.StartedAt = startedAt.Format(time.RFC3339)
	if finishedAt != nil {
		job.FinishedAt = finishedAt.Format(time.RFC3339)
	}
	return job, true
}
