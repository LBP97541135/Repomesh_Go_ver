-- Durable scan jobs: the in-process JobRegistry loses every job on restart,
-- so the frontend polling an old id sees a permanent 404. A mirror table
-- keeps the terminal states (and progress) queryable across restarts.
CREATE TABLE IF NOT EXISTS repomesh_scan.scan_jobs (
    id text PRIMARY KEY,
    kind text NOT NULL,
    url text NOT NULL,
    status text NOT NULL,
    total int NOT NULL DEFAULT 0,
    scanned int NOT NULL DEFAULT 0,
    last_scanned_repository text NOT NULL DEFAULT '',
    registered int NOT NULL DEFAULT 0,
    skipped int NOT NULL DEFAULT 0,
    failed int NOT NULL DEFAULT 0,
    error text NOT NULL DEFAULT '',
    started_at timestamptz NOT NULL DEFAULT now(),
    finished_at timestamptz
);
CREATE INDEX IF NOT EXISTS idx_scan_jobs_started ON repomesh_scan.scan_jobs (started_at DESC);
