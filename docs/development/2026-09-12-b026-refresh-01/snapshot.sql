BEGIN TRANSACTION READ ONLY;
SET LOCAL TIME ZONE 'UTC';

WITH connection_row AS (
    SELECT jsonb_build_object(
        'actor', actor,
        'revision', revision,
        'access_epoch', access_epoch,
        'status', status,
        'refresh_state', refresh_state,
        'credential_committed_at', credential_committed_at,
        'observed_at', observed_at,
        'access_expires_at', access_expires_at,
        'refresh_expires_at', refresh_expires_at,
        'has_refresh_token', refresh_ref <> '',
        'refresh_due_at', access_expires_at - interval '30 seconds'
    ) AS value
    FROM repomesh_access.connections
    WHERE actor = :'actor'
), session_rows AS (
    SELECT COALESCE(jsonb_agg(jsonb_build_object(
        'actor', actor,
        'generation', generation,
        'created_at', created_at,
        'last_active_at', last_active_at,
        'expires_at', expires_at,
        'revoked', revoked
    ) ORDER BY created_at, generation), '[]'::jsonb) AS value
    FROM repomesh_access.sessions
    WHERE actor = :'actor'
), attempt_rows AS (
    SELECT COALESCE(jsonb_agg(jsonb_build_object(
        'id', id,
        'purpose', purpose,
        'state', state,
        'actor', actor,
        'connection_revision', connection_revision,
        'created_at', created_at,
        'observed_at', observed_at
    ) ORDER BY created_at, id), '[]'::jsonb) AS value
    FROM repomesh_access.attempts
    WHERE actor = :'actor'
       OR original_actor = :'actor'
       OR binding IN (SELECT binding FROM repomesh_access.sessions WHERE actor = :'actor')
)
SELECT jsonb_build_object(
    'sampledAt', clock_timestamp(),
    'connection', (SELECT value FROM connection_row),
    'sessions', (SELECT value FROM session_rows),
    'attempts', (SELECT value FROM attempt_rows)
)::text;

COMMIT;
