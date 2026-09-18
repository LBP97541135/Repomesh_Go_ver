CREATE SCHEMA repomesh_secrets;

CREATE TABLE repomesh_secrets.root_keys (
    root_id text PRIMARY KEY CHECK (length(root_id) BETWEEN 1 AND 128),
    fingerprint bytea NOT NULL UNIQUE CHECK (octet_length(fingerprint) = 32),
    active_wrap boolean NOT NULL DEFAULT false,
    wrap_count bigint NOT NULL DEFAULT 0 CHECK (wrap_count BETWEEN 0 AND 1000000),
    registered_at timestamptz NOT NULL DEFAULT now(),
    activated_at timestamptz,
    retired_at timestamptz,
    CHECK (NOT active_wrap OR (activated_at IS NOT NULL AND retired_at IS NULL))
);
CREATE UNIQUE INDEX one_active_root ON repomesh_secrets.root_keys ((true)) WHERE active_wrap;

CREATE TABLE repomesh_secrets.versions (
    version_id text PRIMARY KEY,
    owner_kind text NOT NULL CHECK (length(owner_kind) BETWEEN 1 AND 128),
    owner_id text NOT NULL CHECK (length(owner_id) BETWEEN 1 AND 256),
    purpose text NOT NULL CHECK (length(purpose) BETWEEN 1 AND 128),
    format_version integer NOT NULL CHECK (format_version = 1),
    ciphertext bytea,
    wrapped_dek bytea,
    root_id text NOT NULL REFERENCES repomesh_secrets.root_keys(root_id),
    wrap_revision bigint NOT NULL DEFAULT 1 CHECK (wrap_revision > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    destroyed_at timestamptz,
    CHECK ((destroyed_at IS NULL AND ciphertext IS NOT NULL AND wrapped_dek IS NOT NULL
            AND octet_length(ciphertext) >= 28 AND octet_length(wrapped_dek) = 60)
        OR (destroyed_at IS NOT NULL AND ciphertext IS NULL AND wrapped_dek IS NULL))
);
CREATE INDEX versions_by_root ON repomesh_secrets.versions(root_id, version_id);

CREATE TABLE repomesh_secrets.availability (
    version_id text PRIMARY KEY REFERENCES repomesh_secrets.versions(version_id),
    enabled boolean NOT NULL DEFAULT true,
    access_epoch bigint NOT NULL DEFAULT 1 CHECK (access_epoch > 0),
    checked_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE repomesh_secrets.self_check (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    ciphertext bytea NOT NULL CHECK (octet_length(ciphertext) >= 28),
    wrapped_dek bytea NOT NULL CHECK (octet_length(wrapped_dek) = 60),
    root_id text NOT NULL REFERENCES repomesh_secrets.root_keys(root_id),
    wrap_revision bigint NOT NULL DEFAULT 1 CHECK (wrap_revision > 0)
);
