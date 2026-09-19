-- Discovery's current document is mutable. These records preserve each saved
-- state for an independent collector; migration does not invent past events.
CREATE SCHEMA repomesh_observability;

CREATE TABLE repomesh_observability.facts (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    project_id text NOT NULL,
    issue_id text NOT NULL,
    occurred_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    source_version text NOT NULL CHECK (source_version <> ''),
    -- json preserves the exact serialized bytes used by fingerprint. No JSONB
    -- reformatting may change the immutable evidence on the read/export path.
    snapshot json NOT NULL CHECK (json_typeof(snapshot) = 'object'),
    fingerprint text NOT NULL CHECK (fingerprint ~ '^[0-9a-f]{64}$'),
    FOREIGN KEY (project_id, issue_id)
        REFERENCES repomesh_issues.issues(project_id, id) ON DELETE CASCADE,
    CHECK (COALESCE(snapshot->>'project_id' = project_id, false)),
    CHECK (COALESCE(snapshot->>'issue_id' = issue_id, false))
);

-- Identity order is useful only for listing. A collector must rescan the
-- requested Issue: sequence allocation is not global transaction commit order.
CREATE INDEX observation_facts_issue ON repomesh_observability.facts(project_id, issue_id, id);

CREATE FUNCTION repomesh_observability.immutable_fact() RETURNS trigger AS $$
BEGIN
    -- Preserve the existing explicitly authorized Issue purge transaction.
    IF TG_OP = 'DELETE' AND current_setting('repomesh.purge_mode', true) = 'on' THEN
        RETURN OLD;
    END IF;
    RAISE EXCEPTION 'observation facts are append-only' USING ERRCODE = 'restrict_violation';
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER observation_fact_immutable BEFORE UPDATE OR DELETE
    ON repomesh_observability.facts FOR EACH ROW
    EXECUTE FUNCTION repomesh_observability.immutable_fact();

COMMENT ON TABLE repomesh_observability.facts IS
    'Committed discovery provenance; no historical backfill, no external telemetry. Source versions define snapshot fields.';
