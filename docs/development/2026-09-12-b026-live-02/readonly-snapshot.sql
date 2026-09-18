BEGIN TRANSACTION READ ONLY;

SELECT
    clock_timestamp() AS sampled_at;

SELECT
    id,
    purpose,
    state,
    reason,
    created_at,
    observed_at
FROM repomesh_access.attempts
ORDER BY created_at, id;

SELECT
    actor,
    revision,
    access_epoch,
    status,
    refresh_state,
    credential_committed_at,
    observed_at,
    access_expires_at,
    refresh_expires_at,
    (refresh_ref <> '') AS has_refresh_token,
    (access_expires_at - interval '30 seconds') AS refresh_due_at,
    (access_expires_at IS NOT NULL
        AND access_expires_at - interval '30 seconds' <= clock_timestamp()) AS access_refresh_due
FROM repomesh_access.connections
ORDER BY actor;

SELECT
    actor,
    generation,
    created_at,
    last_active_at,
    expires_at,
    revoked
FROM repomesh_access.sessions
ORDER BY created_at, actor;

COMMIT;
