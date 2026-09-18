-- B10 extension: agent-as-worker. One agent_runs row binds an attempt to one
-- concrete coding agent process (claude/codex) launched by the host executor
-- inside the attempt workspace. The agent never receives SCM credentials: the
-- task package carries goal/acceptance/scope/configuration reference only, and
-- commits stay in the workspace for the platform delivery layer to push.

CREATE SCHEMA IF NOT EXISTS repomesh_execution;

-- 5. agent_runs ----------------------------------------------------------------
CREATE TABLE repomesh_execution.agent_runs (
    id text PRIMARY KEY,
    attempt_id text NOT NULL,
    agent_kind text NOT NULL CHECK (agent_kind IN ('claude_cli','codex_cli')),
    command text NOT NULL,
    workspace text NOT NULL,
    task_package_ref text NOT NULL,
    pid bigint,
    exit_code integer,
    session_ref text,
    state text NOT NULL CHECK (state IN ('pending','running','exited','killed','failed_launch')),
    started_at timestamptz,
    exited_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT agent_runs_attempt_fk FOREIGN KEY (attempt_id)
        REFERENCES repomesh_execution.attempts(id),
    CONSTRAINT agent_runs_lifecycle CHECK (
        (state = 'running' AND pid IS NOT NULL AND started_at IS NOT NULL)
        OR (state IN ('exited','killed') AND exit_code IS NOT NULL AND exited_at IS NOT NULL)
        OR (state = 'pending' AND pid IS NULL)
        OR (state = 'failed_launch' AND started_at IS NULL)
    )
);

-- One live agent run per attempt at a time.
CREATE UNIQUE INDEX agent_runs_one_live_per_attempt
    ON repomesh_execution.agent_runs(attempt_id)
    WHERE state IN ('pending','running');

-- Extend attempt_events kinds with agent lifecycle evidence. The original
-- inline anonymous CHECK is located dynamically and replaced by a named one.
DO $$
DECLARE
    old_name text;
BEGIN
    SELECT conname INTO old_name FROM pg_constraint
        WHERE conrelid = 'repomesh_execution.attempt_events'::regclass
          AND contype = 'c'
          AND pg_get_constraintdef(oid) LIKE '%environment_prepared%';
    IF old_name IS NOT NULL THEN
        EXECUTE format('ALTER TABLE repomesh_execution.attempt_events DROP CONSTRAINT %I', old_name);
    END IF;
END $$;
ALTER TABLE repomesh_execution.attempt_events
    ADD CONSTRAINT attempt_events_kind_allowed
    CHECK (kind IN ('reserved','environment_prepared','launch_verified','started','agent_started','agent_exited','stop_requested','stopped','failed','observed_unknown'));
-- source gains 'agent' for run lifecycle evidence emitted by the executor on
-- behalf of the agent process. The original inline anonymous CHECK cannot be
-- dropped by name, so the column is re-added with the widened set via a
-- rewrite-free constraint swap: new named CHECK replaces the old one by
-- dropping the auto-generated constraint name.
DO $$
DECLARE
    old_name text;
BEGIN
    SELECT conname INTO old_name FROM pg_constraint
        WHERE conrelid = 'repomesh_execution.attempt_events'::regclass
          AND contype = 'c'
          AND pg_get_constraintdef(oid) LIKE '%coordinator%';
    IF old_name IS NOT NULL THEN
        EXECUTE format('ALTER TABLE repomesh_execution.attempt_events DROP CONSTRAINT %I', old_name);
    END IF;
END $$;
ALTER TABLE repomesh_execution.attempt_events
    ADD CONSTRAINT attempt_events_source_check
    CHECK (source IN ('coordinator','host_executor','agent'));
