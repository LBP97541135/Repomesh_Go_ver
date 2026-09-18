-- Discovery chain state (contract v0.4) and issue archive tombstone (v0.5).
-- One discovery row per issue: the four step blocks, approval and the
-- materialization receipt all live in one JSONB document with the step
-- cursor, so the read model derives step/step_state from a single fact.
CREATE TABLE IF NOT EXISTS repomesh_issues.issue_discoveries (
    issue_id text PRIMARY KEY,
    project_id text NOT NULL,
    requirement_text text NOT NULL DEFAULT '',
    analyzed_requirement text,
    analysis jsonb,
    candidates jsonb,
    classification jsonb,
    plan jsonb,
    approval jsonb NOT NULL DEFAULT '{}'::jsonb,
    classification_evidence_version text,
    effective_tiers jsonb NOT NULL DEFAULT '[]'::jsonb,
    integration jsonb,
    materialization jsonb,
    idempotency_ledger jsonb NOT NULL DEFAULT '{}'::jsonb,
    updated_at timestamptz NOT NULL DEFAULT now()
);

ALTER TABLE repomesh_issues.issues ADD COLUMN IF NOT EXISTS archived_at timestamptz;

-- Purge audit: one row per purged issue (the audit trail purge promises).
CREATE TABLE IF NOT EXISTS repomesh_issues.issue_purge_log (
    issue_id text PRIMARY KEY,
    project_id text NOT NULL,
    snapshots int NOT NULL DEFAULT 0,
    decision_chain_nodes int NOT NULL DEFAULT 0,
    audit_events int NOT NULL DEFAULT 0,
    purged_by text NOT NULL DEFAULT '',
    purged_at timestamptz NOT NULL DEFAULT now()
);
