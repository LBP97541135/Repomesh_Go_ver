-- B10: execution attempts, worker occupancy, and host resource lifecycle
-- (repomesh_execution). Per ADR-0017 (atomic reservation in one short
-- transaction, re-verify before launch) and ADR-0013 (restricted host
-- executor owns containers/dirs/network; web and agents never touch the
-- Docker socket). Writes to formal state stay scoped: only the coordinator
-- role writes attempt transitions; executors write observations only.

CREATE SCHEMA IF NOT EXISTS repomesh_execution;

-- 1. workers ------------------------------------------------------------------
-- One registered execution slot on one host. Single active attempt at a time
-- (ADR-0017 single-worker-single-active-task).
CREATE TABLE repomesh_execution.workers (
    id text PRIMARY KEY,
    host text NOT NULL,
    kind text NOT NULL CHECK (kind = 'host_executor'),
    concurrency_limit integer NOT NULL DEFAULT 1 CHECK (concurrency_limit = 1),
    active_attempts integer NOT NULL DEFAULT 0 CHECK (active_attempts >= 0),
    registered_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    heartbeat_at timestamptz,
    retired_at timestamptz,
    CHECK (retired_at IS NULL OR retired_at IS NOT NULL)
);

-- 2. attempts -----------------------------------------------------------------
-- One controlled execution attempt for one issue round. Reservation,
-- launch verification, running, and terminal states are all written by the
-- coordinator; executors never advance these.
CREATE TABLE repomesh_execution.attempts (
    id text PRIMARY KEY,
    project_id text NOT NULL,
    issue_id text NOT NULL,
    worker_id text NOT NULL REFERENCES repomesh_execution.workers(id),
    configuration_revision text NOT NULL,
    state text NOT NULL CHECK (state IN ('reserved','preparing','launch_verified','running','stop_requested','stopped','failed','unknown')),
    reservation_generation bigint NOT NULL DEFAULT 1 CHECK (reservation_generation > 0),
    reserved_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    launch_verified_at timestamptz,
    stopped_at timestamptz,
    fail_reason text,
    CONSTRAINT attempts_issue_fk FOREIGN KEY (project_id, issue_id)
        REFERENCES repomesh_issues.issues(project_id, id) DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT attempts_issue_unique UNIQUE (project_id, issue_id, reservation_generation)
);

-- Reservation completion requires the pinned configuration to match the
-- issue's frozen initial revision (P9 closure carries into execution).
CREATE OR REPLACE FUNCTION repomesh_execution.attempt_configuration_matches() RETURNS trigger AS $$
BEGIN
    IF NEW.state IN ('launch_verified','running','stop_requested','stopped') THEN
        IF NOT EXISTS (
            SELECT 1 FROM repomesh_issues.issues i
            WHERE i.project_id = NEW.project_id AND i.id = NEW.issue_id
              AND i.initial_configuration_revision = NEW.configuration_revision
        ) THEN
            RAISE EXCEPTION 'attempt_configuration_matches: configuration revision does not match the issue pinned revision'
                USING ERRCODE = 'check_violation';
        END IF;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE CONSTRAINT TRIGGER attempt_configuration_matches
AFTER INSERT OR UPDATE ON repomesh_execution.attempts
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION repomesh_execution.attempt_configuration_matches();

-- 3. attempt_events -----------------------------------------------------------
-- Append-only lifecycle evidence; observations are never authoritative state.
CREATE TABLE repomesh_execution.attempt_events (
    attempt_id text NOT NULL,
    sequence integer NOT NULL CHECK (sequence > 0),
    kind text NOT NULL CHECK (kind IN ('reserved','environment_prepared','launch_verified','started','stop_requested','stopped','failed','observed_unknown')),
    source text NOT NULL CHECK (source IN ('coordinator','host_executor')),
    payload jsonb,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT attempt_events_pk PRIMARY KEY (attempt_id, sequence),
    CONSTRAINT attempt_events_attempt_fk FOREIGN KEY (attempt_id)
        REFERENCES repomesh_execution.attempts(id)
);

-- 4. host_resources -----------------------------------------------------------
-- Every container, directory, volume, and network is owned by exactly one
-- lifecycle manager (ADR-0013: same resource is never managed by two
-- controllers). Write-capability revocation is a first-class column.
CREATE TABLE repomesh_execution.host_resources (
    id text PRIMARY KEY,
    attempt_id text NOT NULL,
    kind text NOT NULL CHECK (kind IN ('container','directory','volume','network')),
    external_ref text NOT NULL,
    lifecycle_owner text NOT NULL CHECK (lifecycle_owner = 'host_executor'),
    state text NOT NULL CHECK (state IN ('provisioned','released','orphan_suspected')),
    write_enabled boolean NOT NULL DEFAULT true,
    provisioned_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    released_at timestamptz,
    CONSTRAINT host_resources_attempt_fk FOREIGN KEY (attempt_id)
        REFERENCES repomesh_execution.attempts(id),
    CONSTRAINT host_resources_release CHECK (
        (state = 'provisioned' AND released_at IS NULL)
        OR (state IN ('released','orphan_suspected'))
    )
);

-- An attempt cannot reach stopped while any resource still has write enabled
-- or is not released: stopping and write revocation are verified, not assumed.
CREATE OR REPLACE FUNCTION repomesh_execution.attempt_stop_requires_cleanup() RETURNS trigger AS $$
BEGIN
    IF NEW.state = 'stopped' THEN
        IF EXISTS (
            SELECT 1 FROM repomesh_execution.host_resources r
            WHERE r.attempt_id = NEW.id AND (r.write_enabled = true OR r.state = 'provisioned')
        ) THEN
            RAISE EXCEPTION 'attempt_stop_requires_cleanup: stopped attempt still has provisioned or write-enabled resources'
                USING ERRCODE = 'check_violation';
        END IF;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE CONSTRAINT TRIGGER attempt_stop_requires_cleanup
AFTER UPDATE ON repomesh_execution.attempts
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION repomesh_execution.attempt_stop_requires_cleanup();
