-- 0018: fix BEFORE trigger allow-path RETURN NULL bugs shipped in 0015/0016.
-- Postgres semantics: a BEFORE ROW trigger returning NULL silently skips the
-- row operation, so every legal UPDATE on these tables was silently dropped
-- (found via B06 destructive SQL case 3: operation cleanup + resurrection
-- produced no error). 0017 is fixed in place (never committed); 0015/0016 are
-- already on main, so their functions are redefined here via CREATE OR
-- REPLACE FUNCTION, which does not disturb the migration checksum ledger.
--
-- Affected allow-path RETURN NULLs corrected here:
--   repomesh_modelbudget.reservation_state_guard   (BEFORE UPDATE ON test_reservations)
--   repomesh_models.preview_consume_guard          (BEFORE UPDATE ON previews)
--   repomesh_models.tests_state_guard              (BEFORE UPDATE ON tests)
--   repomesh_models.dispatch_fact_guard            (BEFORE UPDATE ON test_dispatch)
--   repomesh_models.application_receipt_guard      (BEFORE UPDATE ON application_operations)
--   repomesh_sources.egress_urls_distinct          (BEFORE INSERT ON egress_policy_versions)
--   repomesh_modelbudget.window_limit_only_decreases (BEFORE UPDATE ON windows)

CREATE OR REPLACE FUNCTION repomesh_modelbudget.reservation_state_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF OLD.state = 'consumed' AND NEW.state <> 'consumed' THEN
        RAISE EXCEPTION 'consumed reservation is final';
    END IF;
    IF OLD.state = 'released' AND NEW.state <> 'released' THEN
        RAISE EXCEPTION 'released reservation is final';
    END IF;
    IF OLD.state = 'reserved' AND NEW.state = 'released' AND NEW.settled_at IS NULL THEN
        RAISE EXCEPTION 'release requires settled_at';
    END IF;
    IF OLD.state = 'reserved' AND NEW.state = 'consumed' AND NEW.settled_at IS NULL THEN
        RAISE EXCEPTION 'consume requires settled_at';
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION repomesh_models.preview_consume_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF OLD.consumed_by_operation IS NOT NULL AND NEW.consumed_by_operation IS DISTINCT FROM OLD.consumed_by_operation THEN
        RAISE EXCEPTION 'preview consumption is final';
    END IF;
    IF OLD.consumed_by_operation IS NULL AND NEW.consumed_by_operation IS NOT NULL AND NEW.consumed_at IS NULL THEN
        RAISE EXCEPTION 'preview consume requires consumed_at';
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION repomesh_models.tests_state_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.actor <> OLD.actor OR NEW.test_id <> OLD.test_id
        OR NEW.provider_id IS DISTINCT FROM OLD.provider_id
        OR NEW.provider_revision IS DISTINCT FROM OLD.provider_revision
        OR NEW.model_row_id IS DISTINCT FROM OLD.model_row_id
        OR NEW.preview_id IS DISTINCT FROM OLD.preview_id
        OR NEW.accepted_at IS DISTINCT FROM OLD.accepted_at THEN
        RAISE EXCEPTION 'test identity and acceptance are immutable';
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION repomesh_models.dispatch_fact_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF OLD.may_have_sent_at IS NOT NULL AND NEW.may_have_sent_at IS DISTINCT FROM OLD.may_have_sent_at THEN
        RAISE EXCEPTION 'may_have_sent_at is final once set';
    END IF;
    IF OLD.capability_revoked_at IS NOT NULL AND
        (NEW.capability_revoked_at IS DISTINCT FROM OLD.capability_revoked_at
         OR NEW.revocation_evidence_digest IS DISTINCT FROM OLD.revocation_evidence_digest) THEN
        RAISE EXCEPTION 'capability revocation is final';
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION repomesh_models.application_receipt_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.outcome <> OLD.outcome
        OR NEW.project_revision_after IS DISTINCT FROM OLD.project_revision_after
        OR NEW.rejection_code IS DISTINCT FROM OLD.rejection_code
        OR NEW.candidate_snapshot IS DISTINCT FROM OLD.candidate_snapshot
        OR NEW.committed_at IS DISTINCT FROM OLD.committed_at THEN
        RAISE EXCEPTION 'application receipt is immutable';
    END IF;
    IF OLD.removed_at IS NOT NULL AND NEW.removed_at IS DISTINCT FROM OLD.removed_at THEN
        RAISE EXCEPTION 'removed_at is final once set';
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION repomesh_sources.egress_urls_distinct() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF (SELECT count(DISTINCT e) FROM unnest(NEW.approved_base_urls) AS e)
        <> array_length(NEW.approved_base_urls, 1) THEN
        RAISE EXCEPTION 'egress approved_base_urls must not contain duplicates';
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION repomesh_modelbudget.window_limit_only_decreases() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.daily_limit > OLD.daily_limit THEN
        RAISE EXCEPTION 'model budget window limit can only decrease';
    END IF;
    RETURN NEW;
END;
$$;
