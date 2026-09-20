ALTER TABLE repomesh_execution.agent_runs DROP CONSTRAINT agent_runs_agent_kind_check;
ALTER TABLE repomesh_execution.agent_runs ADD CONSTRAINT agent_runs_agent_kind_check
    CHECK (agent_kind IN ('claude_cli','codex_cli','test_agent','planning_agent','review_agent'));

CREATE SCHEMA repomesh_typesafe;

CREATE TABLE repomesh_typesafe.settings (
    project_id text PRIMARY KEY REFERENCES repomesh_projects.projects(id),
    revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
    enabled boolean NOT NULL DEFAULT false,
    model text NOT NULL,
    secret_version text REFERENCES repomesh_secrets.versions(version_id),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    checked_at timestamptz,
    check_status text NOT NULL DEFAULT 'not_checked',
    CHECK (NOT enabled OR secret_version IS NOT NULL)
);

CREATE TABLE repomesh_typesafe.run_grants (
    run_id text PRIMARY KEY REFERENCES repomesh_execution.agent_runs(id),
    project_id text NOT NULL REFERENCES repomesh_typesafe.settings(project_id),
    issue_id text NOT NULL,
    purpose text NOT NULL CHECK (purpose IN ('test','code_review')),
    task_id text NOT NULL DEFAULT '',
    revision bigint NOT NULL,
    token_hash bytea NOT NULL UNIQUE CHECK (octet_length(token_hash) = 32),
    skill_hash text NOT NULL,
    expires_at timestamptz NOT NULL,
    revoked_at timestamptz,
    calls integer NOT NULL DEFAULT 0 CHECK (calls BETWEEN 0 AND 8),
    FOREIGN KEY (project_id, issue_id) REFERENCES repomesh_issues.issues(project_id, id)
);

CREATE TABLE repomesh_typesafe.evaluations (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id text NOT NULL REFERENCES repomesh_projects.projects(id),
    issue_id text NOT NULL,
    purpose text NOT NULL CHECK (purpose IN ('test','code_review')),
    task_id text NOT NULL DEFAULT '',
    run_id text NOT NULL REFERENCES repomesh_execution.agent_runs(id),
    request_id text NOT NULL,
    revision bigint NOT NULL,
    skill_hash text NOT NULL,
    template_version text NOT NULL,
    input jsonb NOT NULL,
    input_hash text NOT NULL,
    status text NOT NULL CHECK (status IN ('pending','completed','failed','unknown','unavailable')),
    error_code text NOT NULL DEFAULT '',
    response jsonb,
    latency_ms bigint NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    completed_at timestamptz,
    UNIQUE (run_id, request_id),
    FOREIGN KEY (project_id, issue_id) REFERENCES repomesh_issues.issues(project_id, id)
);
CREATE INDEX typesafe_evaluations_issue ON repomesh_typesafe.evaluations(project_id, issue_id, created_at DESC);
