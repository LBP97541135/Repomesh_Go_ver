-- Issue creation two-phase write fix.
--
-- The creation pipeline reserves repomesh_issues.creation_operations with a
-- placeholder INSERT (result columns NULL) inside the commit transaction and
-- fills the result columns with a single UPDATE at the end of the same
-- transaction (completeCreationOperation). immutable_creation_identity as
-- written in 0017 treats every change to the result columns as a violation,
-- including the legitimate NULL -> value transition performed by that very
-- pipeline, so every first-attempt issue creation fails and rolls back.
--
-- This migration relaxes only that impossible case: result columns may move
-- from NULL to a value (the reservation completing inside its own creation
-- transaction) but never change once set, and can never go back to NULL.

CREATE OR REPLACE FUNCTION repomesh_issues.immutable_creation_identity() RETURNS trigger AS $$
BEGIN
    -- removed_at may only transition from NULL to a timestamp (cleanup); the
    -- cleanup CHECK pins canonical/exact/receipt to NULL, so once removed the
    -- row can never carry inputs again and the transition cannot be reversed.
    IF NEW.removed_at IS NULL AND OLD.removed_at IS NOT NULL THEN
        RAISE EXCEPTION 'immutable_creation_identity: removed operations cannot be resurrected'
            USING ERRCODE = 'check_violation';
    END IF;
    -- Two-phase completion: NULL -> value is the reservation being completed
    -- by completeCreationOperation within the same creating transaction.
    -- Anything else (value -> different value, value -> NULL) is a violation.
    IF (NEW.issue_id IS NULL) <> (OLD.issue_id IS NULL)
       OR (NEW.main_changeset_id IS NULL) <> (OLD.main_changeset_id IS NULL)
       OR (NEW.conversation_id IS NULL) <> (OLD.conversation_id IS NULL)
       OR (NEW.initial_configuration_revision IS NULL) <> (OLD.initial_configuration_revision IS NULL) THEN
        IF OLD.issue_id IS NOT NULL OR OLD.main_changeset_id IS NOT NULL
           OR OLD.conversation_id IS NOT NULL OR OLD.initial_configuration_revision IS NOT NULL THEN
            RAISE EXCEPTION 'immutable_creation_identity: completed creation_operations result columns are immutable'
                USING ERRCODE = 'check_violation';
        END IF;
    END IF;
    IF NEW.project_id <> OLD.project_id OR NEW.actor <> OLD.actor
       OR NEW.entry <> OLD.entry OR NEW.creation_id <> OLD.creation_id
       OR NEW.id IS DISTINCT FROM OLD.id
       OR NEW.schema_version <> OLD.schema_version
       OR (OLD.issue_id IS NOT NULL AND NEW.issue_id IS DISTINCT FROM OLD.issue_id)
       OR (OLD.main_changeset_id IS NOT NULL AND NEW.main_changeset_id IS DISTINCT FROM OLD.main_changeset_id)
       OR (OLD.conversation_id IS NOT NULL AND NEW.conversation_id IS DISTINCT FROM OLD.conversation_id)
       OR (OLD.initial_configuration_revision IS NOT NULL AND NEW.initial_configuration_revision IS DISTINCT FROM OLD.initial_configuration_revision) THEN
        RAISE EXCEPTION 'immutable_creation_identity: identity and result columns on creation_operations are immutable'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
