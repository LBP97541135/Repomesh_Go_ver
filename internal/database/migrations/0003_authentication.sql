CREATE SCHEMA repomesh_access;

CREATE TABLE repomesh_access.accounts (
    id text PRIMARY KEY,
    github_id bigint NOT NULL UNIQUE CHECK (github_id > 0),
    display_name text NOT NULL,
    disabled boolean NOT NULL DEFAULT false
);

CREATE TABLE repomesh_access.bindings (
    hash text PRIMARY KEY CHECK (length(hash) = 64),
    identity_generation bigint NOT NULL DEFAULT 0,
    attempt_generation bigint NOT NULL DEFAULT 0,
    last_actor text REFERENCES repomesh_access.accounts(id),
    expires_at timestamptz NOT NULL
);

CREATE TABLE repomesh_access.sessions (
    hash text PRIMARY KEY CHECK (length(hash) = 64),
    actor text NOT NULL REFERENCES repomesh_access.accounts(id),
    binding text NOT NULL REFERENCES repomesh_access.bindings(hash),
    generation bigint NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    last_active_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    revoked boolean NOT NULL DEFAULT false
);
CREATE INDEX sessions_binding ON repomesh_access.sessions(binding);

CREATE TABLE repomesh_access.connections (
    actor text PRIMARY KEY REFERENCES repomesh_access.accounts(id),
    revision text NOT NULL,
    credential_committed_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    access_epoch bigint NOT NULL DEFAULT 1,
    access_ref text NOT NULL,
    refresh_ref text NOT NULL DEFAULT '',
    access_expires_at timestamptz,
    refresh_expires_at timestamptz,
    status text NOT NULL CHECK (status IN ('connected', 'missing', 'unknown')),
    observed_at timestamptz,
    refresh_state text NOT NULL DEFAULT 'idle' CHECK (refresh_state IN ('idle', 'claimed', 'unknown'))
);

CREATE TABLE repomesh_access.attempts (
    binding text NOT NULL REFERENCES repomesh_access.bindings(hash),
    id text NOT NULL,
    purpose text NOT NULL CHECK (purpose IN ('login', 'reconnect')),
    actor text,
    original_actor text,
    expected_github_id bigint,
    expected_connection_revision text,
    identity_generation bigint NOT NULL,
    attempt_generation bigint NOT NULL,
    destination jsonb NOT NULL,
    state_hash text NOT NULL UNIQUE,
    material_ref text NOT NULL DEFAULT '',
    token_ref text NOT NULL DEFAULT '',
    state text NOT NULL CHECK (state IN ('pending', 'exchanging', 'identity_pending', 'unknown', 'confirmed', 'cancelled', 'rejected', 'expired', 'superseded')),
    reason text,
    connection_revision text,
    observed_at timestamptz,
    next_run_at timestamptz NOT NULL DEFAULT now(),
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    PRIMARY KEY (binding, id)
);
CREATE INDEX attempts_pending ON repomesh_access.attempts(state, expires_at);
CREATE INDEX attempts_actor_started ON repomesh_access.attempts(actor, created_at);

CREATE TABLE repomesh_access.discovery_batches (
    id text PRIMARY KEY,
    actor text NOT NULL REFERENCES repomesh_access.accounts(id),
    connection_revision text NOT NULL,
    access_epoch bigint NOT NULL,
    query text NOT NULL,
    upstream_page integer NOT NULL DEFAULT 1 CHECK (upstream_page > 0),
    complete boolean NOT NULL DEFAULT false,
    had_success boolean NOT NULL DEFAULT false,
    delivered boolean NOT NULL DEFAULT false,
    claim_version bigint NOT NULL DEFAULT 0,
    lease_until timestamptz,
    next_run_at timestamptz NOT NULL DEFAULT now(),
    observed_at timestamptz,
    expires_at timestamptz NOT NULL
);
CREATE INDEX discovery_work ON repomesh_access.discovery_batches(next_run_at) WHERE NOT complete;
CREATE INDEX discovery_reuse ON repomesh_access.discovery_batches(actor, connection_revision, access_epoch, query);

CREATE TABLE repomesh_access.discovered_repositories (
    batch text NOT NULL REFERENCES repomesh_access.discovery_batches(id),
    github_id bigint NOT NULL CHECK (github_id > 0),
    owner text NOT NULL,
    name text NOT NULL,
    full_name text NOT NULL,
    observed_at timestamptz NOT NULL,
    PRIMARY KEY (batch, github_id)
);

CREATE TABLE repomesh_access.discovery_cursors (
    id text PRIMARY KEY,
    batch text NOT NULL REFERENCES repomesh_access.discovery_batches(id),
    actor text NOT NULL,
    query text NOT NULL,
    after_id bigint NOT NULL
);

CREATE TABLE repomesh_access.app_credentials (
    app_id text NOT NULL,
    client_id text NOT NULL,
    purpose text NOT NULL,
    fingerprint text NOT NULL,
    version_ref text NOT NULL,
    PRIMARY KEY (app_id, client_id, purpose, fingerprint)
);
