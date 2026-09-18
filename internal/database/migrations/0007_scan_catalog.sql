CREATE SCHEMA repomesh_scan;

-- Shared repositories table (merged): identity + scan card (scan domain)
-- + SCM polling cursor (delivery domain). Each domain writes only its own
-- columns. One row per scanned repository. metadata holds the AutoCard payload and is
-- written only through scan.autoCardPayload/autoCardFromPayload (single
-- serializer). fingerprint is the repository HEAD SHA at scan time; the
-- incremental gate compares it before re-fetching anything.
CREATE TABLE repomesh_scan.repositories (
    id text PRIMARY KEY,
    name text NOT NULL UNIQUE,
    url text NOT NULL UNIQUE,
    description text NOT NULL DEFAULT '',
    topics jsonb NOT NULL DEFAULT '[]'::jsonb,
    languages jsonb NOT NULL DEFAULT '[]'::jsonb,
    fingerprint text NOT NULL DEFAULT '',
    profiled_at timestamptz NOT NULL DEFAULT now(),
    metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
    test_commands jsonb NOT NULL DEFAULT '[]'::jsonb,
    test_paths jsonb NOT NULL DEFAULT '[]'::jsonb,
    -- SCM polling cursor (delivery domain writes; scan never touches).
    poll_last_at timestamptz,
    poll_next_at timestamptz,
    poll_failures integer NOT NULL DEFAULT 0
);
