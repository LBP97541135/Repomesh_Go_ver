CREATE OR REPLACE FUNCTION repomesh_models.require_complete_save() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE complete boolean;
BEGIN
    SELECT CASE
        WHEN removed_at IS NOT NULL THEN
            receipt IS NULL AND canonical_nonsecret IS NULL AND input_vault_version IS NULL
        WHEN kind = 'save_closed' THEN
            outcome = 'closed_without_save' AND receipt IS NOT NULL AND target IS NULL
            AND canonical_nonsecret IS NULL AND input_vault_version IS NULL AND schema_version IS NULL
        WHEN kind = 'save_input' AND outcome IN ('committed', 'rejected') THEN
            schema_version IS NOT NULL AND canonical_nonsecret IS NOT NULL AND receipt IS NOT NULL
            AND (outcome <> 'committed' OR target IS NOT NULL)
            AND (
                CASE convert_from(canonical_nonsecret, 'UTF8')::jsonb->>'replace'
                    WHEN 'true' THEN input_vault_version IS NOT NULL
                    WHEN 'false' THEN input_vault_version IS NULL
                    ELSE false
                END
            )
        ELSE false
    END
    INTO complete
    FROM repomesh_models.save_operations WHERE actor = NEW.actor AND save_id = NEW.save_id;
    IF complete IS DISTINCT FROM true THEN
        RAISE EXCEPTION 'incomplete RepoMesh model save';
    END IF;
    RETURN NULL;
END;
$$;
