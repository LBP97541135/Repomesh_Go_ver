-- B06 U06.1: issue creation schema (repomesh_issues).
-- Design source: docs/development/2026-09-13-b04-b06-design-01/migration-design.md
-- lines 76-132. Thirteen tables in this schema plus the project-side trigger
-- configuration_owner_and_binding_consistent. Named constraint triggers below
-- are proven with destructive SQL after migration (design line 114).

CREATE SCHEMA IF NOT EXISTS repomesh_issues;

-- 1. conversations -----------------------------------------------------------
CREATE TABLE repomesh_issues.conversations (
    id text PRIMARY KEY,
    project_id text NOT NULL REFERENCES repomesh_projects.projects(id),
    title text,
    title_redacted_at timestamptz,
    created_by_operation_id text NOT NULL,
    title_origin_operation_id text,
    content_scope_revision text NOT NULL,
    removed_at timestamptz,
    CONSTRAINT conversations_title_state CHECK (
        (title IS NOT NULL AND title <> '' AND title_redacted_at IS NULL)
        OR (title IS NULL AND title_redacted_at IS NOT NULL)
    ),
    CONSTRAINT conversations_project_id UNIQUE (project_id, id)
);

-- 2. issues ------------------------------------------------------------------
CREATE TABLE repomesh_issues.issues (
    id text PRIMARY KEY,
    project_id text NOT NULL REFERENCES repomesh_projects.projects(id),
    number bigint NOT NULL CHECK (number > 0),
    title text NOT NULL,
    description text NOT NULL,
    criteria jsonb NOT NULL,
    revision text NOT NULL,
    main_conversation_id text NOT NULL,
    main_changeset_id text NOT NULL,
    initial_configuration_revision text NOT NULL,
    creation_operation_id text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    removed_at timestamptz,
    CONSTRAINT issues_project_number UNIQUE (project_id, number),
    CONSTRAINT issues_project_id UNIQUE (project_id, id),
    CONSTRAINT issues_project_id_configuration UNIQUE (project_id, id, initial_configuration_revision)
);

-- 3. creation_operations -----------------------------------------------------
CREATE TABLE repomesh_issues.creation_operations (
    project_id text NOT NULL REFERENCES repomesh_projects.projects(id),
    actor text NOT NULL REFERENCES repomesh_access.accounts(id),
    entry text NOT NULL CHECK (entry = 'issue_page'),
    creation_id text NOT NULL,
    id text NOT NULL,
    schema_version integer NOT NULL CHECK (schema_version = 1),
    canonical_input bytea,
    exact_input bytea,
    input_digest bytea,
    issue_id text,
    main_changeset_id text,
    conversation_id text,
    initial_configuration_revision text,
    receipt jsonb,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    removed_at timestamptz,
    CONSTRAINT creation_operations_pk PRIMARY KEY (project_id, actor, entry, creation_id),
    CONSTRAINT creation_operations_id UNIQUE (id),
    CONSTRAINT creation_operations_project_id UNIQUE (project_id, id),
    CONSTRAINT creation_operations_project_id_issue_id UNIQUE (project_id, id, issue_id),
    CONSTRAINT creation_operations_cleanup_state CHECK (
        (removed_at IS NULL)
        OR (canonical_input IS NULL AND exact_input IS NULL AND receipt IS NULL)
    )
);

-- 4. changesets --------------------------------------------------------------
CREATE TABLE repomesh_issues.changesets (
    id text PRIMARY KEY,
    project_id text NOT NULL,
    issue_id text NOT NULL,
    kind text NOT NULL CHECK (kind = 'main'),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT changesets_project_issue_id UNIQUE (project_id, issue_id, id),
    CONSTRAINT changesets_project_issue UNIQUE (project_id, issue_id)
);

ALTER TABLE repomesh_issues.changesets
    ADD CONSTRAINT changesets_issue_fk
    FOREIGN KEY (project_id, issue_id)
    REFERENCES repomesh_issues.issues(project_id, id)
    DEFERRABLE INITIALLY DEFERRED;

-- 5. issue_repository_scope --------------------------------------------------
CREATE TABLE repomesh_issues.issue_repository_scope (
    issue_id text NOT NULL,
    repository_id text NOT NULL,
    project_id text NOT NULL,
    scope_revision text NOT NULL,
    CONSTRAINT issue_repository_scope_pk PRIMARY KEY (issue_id, repository_id)
);

ALTER TABLE repomesh_issues.issue_repository_scope
    ADD CONSTRAINT issue_repository_scope_issue_fk
    FOREIGN KEY (project_id, issue_id)
    REFERENCES repomesh_issues.issues(project_id, id)
    DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE repomesh_issues.issue_repository_scope
    ADD CONSTRAINT issue_repository_scope_repository_fk
    FOREIGN KEY (project_id, repository_id)
    REFERENCES repomesh_projects.project_repositories(project_id, repository_id);

-- 6. issue_content_scope -----------------------------------------------------
CREATE TABLE repomesh_issues.issue_content_scope (
    issue_id text NOT NULL,
    repository_id text NOT NULL,
    project_id text NOT NULL,
    introduced_by_operation text NOT NULL,
    CONSTRAINT issue_content_scope_pk PRIMARY KEY (issue_id, repository_id)
);

ALTER TABLE repomesh_issues.issue_content_scope
    ADD CONSTRAINT issue_content_scope_issue_fk
    FOREIGN KEY (project_id, issue_id)
    REFERENCES repomesh_issues.issues(project_id, id)
    DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE repomesh_issues.issue_content_scope
    ADD CONSTRAINT issue_content_scope_operation_fk
    FOREIGN KEY (project_id, introduced_by_operation)
    REFERENCES repomesh_issues.creation_operations(project_id, id)
    DEFERRABLE INITIALLY DEFERRED;

-- 7. conversation_content_scope ----------------------------------------------
CREATE TABLE repomesh_issues.conversation_content_scope (
    conversation_id text NOT NULL,
    repository_id text NOT NULL,
    project_id text NOT NULL,
    introduced_by_operation text NOT NULL,
    CONSTRAINT conversation_content_scope_pk PRIMARY KEY (conversation_id, repository_id)
);

ALTER TABLE repomesh_issues.conversation_content_scope
    ADD CONSTRAINT conversation_content_scope_conversation_fk
    FOREIGN KEY (project_id, conversation_id)
    REFERENCES repomesh_issues.conversations(project_id, id)
    DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE repomesh_issues.conversation_content_scope
    ADD CONSTRAINT conversation_content_scope_operation_fk
    FOREIGN KEY (project_id, introduced_by_operation)
    REFERENCES repomesh_issues.creation_operations(project_id, id)
    DEFERRABLE INITIALLY DEFERRED;

-- 8. page_sources ------------------------------------------------------------
CREATE TABLE repomesh_issues.page_sources (
    operation_id text NOT NULL UNIQUE,
    project_id text NOT NULL,
    issue_id text NOT NULL,
    conversation_id text NOT NULL,
    summary text,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT page_sources_project_issue_operation UNIQUE (project_id, issue_id, operation_id)
);

ALTER TABLE repomesh_issues.page_sources
    ADD CONSTRAINT page_sources_issue_fk
    FOREIGN KEY (project_id, issue_id)
    REFERENCES repomesh_issues.issues(project_id, id)
    DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE repomesh_issues.page_sources
    ADD CONSTRAINT page_sources_conversation_fk
    FOREIGN KEY (project_id, conversation_id)
    REFERENCES repomesh_issues.conversations(project_id, id)
    DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE repomesh_issues.page_sources
    ADD CONSTRAINT page_sources_operation_fk
    FOREIGN KEY (project_id, operation_id)
    REFERENCES repomesh_issues.creation_operations(project_id, id)
    DEFERRABLE INITIALLY DEFERRED;

-- 9. conversation_cards ------------------------------------------------------
CREATE TABLE repomesh_issues.conversation_cards (
    source_id text NOT NULL UNIQUE,
    project_id text NOT NULL,
    conversation_id text NOT NULL,
    issue_id text NOT NULL,
    operation_id text NOT NULL,
    summary text,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT conversation_cards_project_conversation_source UNIQUE (project_id, conversation_id, source_id)
);

ALTER TABLE repomesh_issues.conversation_cards
    ADD CONSTRAINT conversation_cards_source_fk
    FOREIGN KEY (source_id)
    REFERENCES repomesh_issues.page_sources(operation_id);
ALTER TABLE repomesh_issues.conversation_cards
    ADD CONSTRAINT conversation_cards_conversation_fk
    FOREIGN KEY (project_id, conversation_id)
    REFERENCES repomesh_issues.conversations(project_id, id)
    DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE repomesh_issues.conversation_cards
    ADD CONSTRAINT conversation_cards_issue_fk
    FOREIGN KEY (project_id, issue_id)
    REFERENCES repomesh_issues.issues(project_id, id)
    DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE repomesh_issues.conversation_cards
    ADD CONSTRAINT conversation_cards_operation_fk
    FOREIGN KEY (project_id, operation_id)
    REFERENCES repomesh_issues.creation_operations(project_id, id)
    DEFERRABLE INITIALLY DEFERRED;

-- 10. continuation_work ------------------------------------------------------
CREATE TABLE repomesh_issues.continuation_work (
    work_id text PRIMARY KEY,
    project_id text NOT NULL,
    issue_id text NOT NULL,
    cause_operation_id text NOT NULL,
    kind text NOT NULL CHECK (kind = 'issue_continue'),
    state text NOT NULL CHECK (state IN ('blocked', 'cancelled')),
    reason text NOT NULL CHECK (reason IN ('INTEGRATION_NOT_AVAILABLE', 'CONTENT_REMOVED')),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    cancelled_at timestamptz,
    CONSTRAINT continuation_work_cause UNIQUE (cause_operation_id, kind, issue_id),
    CONSTRAINT continuation_work_cancelled_state CHECK (
        (state = 'blocked' AND cancelled_at IS NULL)
        OR (state = 'cancelled' AND reason = 'CONTENT_REMOVED' AND cancelled_at IS NOT NULL)
    )
);

ALTER TABLE repomesh_issues.continuation_work
    ADD CONSTRAINT continuation_work_issue_fk
    FOREIGN KEY (project_id, issue_id)
    REFERENCES repomesh_issues.issues(project_id, id)
    DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE repomesh_issues.continuation_work
    ADD CONSTRAINT continuation_work_cause_fk
    FOREIGN KEY (project_id, cause_operation_id, issue_id)
    REFERENCES repomesh_issues.creation_operations(project_id, id, issue_id)
    DEFERRABLE INITIALLY DEFERRED;

-- 11. issue_event_streams ----------------------------------------------------
CREATE TABLE repomesh_issues.issue_event_streams (
    issue_id text PRIMARY KEY,
    project_id text NOT NULL,
    generation integer NOT NULL CHECK (generation > 0),
    last_sequence integer NOT NULL CHECK (last_sequence >= 0)
);

ALTER TABLE repomesh_issues.issue_event_streams
    ADD CONSTRAINT issue_event_streams_issue_fk
    FOREIGN KEY (project_id, issue_id)
    REFERENCES repomesh_issues.issues(project_id, id)
    DEFERRABLE INITIALLY DEFERRED;

-- 12. issue_events -----------------------------------------------------------
CREATE TABLE repomesh_issues.issue_events (
    issue_id text NOT NULL,
    generation integer NOT NULL,
    sequence integer NOT NULL CHECK (sequence > 0),
    kind text NOT NULL CHECK (kind = 'snapshot_invalidated'),
    payload jsonb,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT issue_events_pk PRIMARY KEY (issue_id, generation, sequence)
);

-- stream needs (issue_id, generation) unique target for the FK below
CREATE UNIQUE INDEX issue_event_streams_issue_generation
    ON repomesh_issues.issue_event_streams(issue_id, generation);

ALTER TABLE repomesh_issues.issue_events
    ADD CONSTRAINT issue_events_stream_fk
    FOREIGN KEY (issue_id, generation)
    REFERENCES repomesh_issues.issue_event_streams(issue_id, generation);

-- 13. project_issue_counters -------------------------------------------------
CREATE TABLE repomesh_issues.project_issue_counters (
    project_id text PRIMARY KEY REFERENCES repomesh_projects.projects(id),
    next_number bigint NOT NULL CHECK (next_number > 0)
);

-- Core identity FKs on issues (design lines 95-110) ---------------------------
ALTER TABLE repomesh_issues.issues
    ADD CONSTRAINT issues_initial_configuration_fk
    FOREIGN KEY (project_id, initial_configuration_revision)
    REFERENCES repomesh_projects.configuration_revisions(project_id, revision);
ALTER TABLE repomesh_issues.issues
    ADD CONSTRAINT issues_creation_operation_fk
    FOREIGN KEY (project_id, creation_operation_id)
    REFERENCES repomesh_issues.creation_operations(project_id, id)
    DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE repomesh_issues.issues
    ADD CONSTRAINT issues_main_changeset_fk
    FOREIGN KEY (project_id, id, main_changeset_id)
    REFERENCES repomesh_issues.changesets(project_id, issue_id, id)
    DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE repomesh_issues.issues
    ADD CONSTRAINT issues_main_conversation_fk
    FOREIGN KEY (project_id, main_conversation_id)
    REFERENCES repomesh_issues.conversations(project_id, id);

-- conversations reference creation_operations (placed here because
-- creation_operations is created after conversations)
ALTER TABLE repomesh_issues.conversations
    ADD CONSTRAINT conversations_created_by_operation_fk
    FOREIGN KEY (project_id, created_by_operation_id)
    REFERENCES repomesh_issues.creation_operations(project_id, id)
    DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE repomesh_issues.conversations
    ADD CONSTRAINT conversations_title_origin_operation_fk
    FOREIGN KEY (project_id, title_origin_operation_id)
    REFERENCES repomesh_issues.creation_operations(project_id, id)
    DEFERRABLE INITIALLY DEFERRED;

-- Named constraint triggers (design lines 114-129) -----------------------------
-- Each trigger re-reads the final row state at constraint-check time (not the
-- partial NEW row mid-transaction), per design line 122.

-- 1. creation_aggregate_complete: a committed, live creation operation must
-- reference a complete aggregate: one issue, one main changeset, one main
-- conversation, one page source + card, a non-empty content scope, one
-- blocked continuation work, and at least one creation event. Uncommitted
-- placeholders (issue_id NULL) and removed operations pass.
CREATE OR REPLACE FUNCTION repomesh_issues.creation_aggregate_complete() RETURNS trigger AS $$
DECLARE
    op record;
    n bigint;
    scope_count bigint;
BEGIN
    SELECT * INTO op FROM repomesh_issues.creation_operations
        WHERE project_id = NEW.project_id AND actor = NEW.actor
          AND entry = NEW.entry AND creation_id = NEW.creation_id;
    IF op.removed_at IS NOT NULL THEN RETURN NULL; END IF;
    IF op.issue_id IS NULL THEN RETURN NULL; END IF; -- uncommitted placeholder
    IF op.canonical_input IS NULL OR op.exact_input IS NULL OR op.receipt IS NULL THEN
        RAISE EXCEPTION 'creation_aggregate_complete: committed operation missing canonical input, exact input, or receipt'
            USING ERRCODE = 'check_violation';
    END IF;
    -- schema supports the aggregate
    IF op.schema_version IS DISTINCT FROM 1 THEN
        RAISE EXCEPTION 'creation_aggregate_complete: unsupported schema version %', op.schema_version
            USING ERRCODE = 'check_violation';
    END IF;
    SELECT count(*) INTO n FROM repomesh_issues.issues
        WHERE project_id = op.project_id AND id = op.issue_id
          AND creation_operation_id = op.id
          AND main_changeset_id = op.main_changeset_id
          AND main_conversation_id = op.conversation_id
          AND initial_configuration_revision = op.initial_configuration_revision;
    IF n IS DISTINCT FROM 1 THEN
        RAISE EXCEPTION 'creation_aggregate_complete: issue row missing or identity mismatch for operation %', op.id
            USING ERRCODE = 'check_violation';
    END IF;
    SELECT count(*) INTO n FROM repomesh_issues.changesets
        WHERE project_id = op.project_id AND issue_id = op.issue_id AND id = op.main_changeset_id AND kind = 'main';
    IF n IS DISTINCT FROM 1 THEN
        RAISE EXCEPTION 'creation_aggregate_complete: main changeset missing for operation %', op.id
            USING ERRCODE = 'check_violation';
    END IF;
    SELECT count(*) INTO n FROM repomesh_issues.conversations
        WHERE project_id = op.project_id AND id = op.conversation_id
          AND created_by_operation_id = op.id
          AND title_origin_operation_id = op.id;
    IF n IS DISTINCT FROM 1 THEN
        RAISE EXCEPTION 'creation_aggregate_complete: main conversation missing or wrong origin for operation %', op.id
            USING ERRCODE = 'check_violation';
    END IF;
    SELECT count(*) INTO n FROM repomesh_issues.page_sources
        WHERE project_id = op.project_id AND operation_id = op.id
          AND issue_id = op.issue_id AND conversation_id = op.conversation_id;
    IF n IS DISTINCT FROM 1 THEN
        RAISE EXCEPTION 'creation_aggregate_complete: page source missing for operation %', op.id
            USING ERRCODE = 'check_violation';
    END IF;
    SELECT count(*) INTO n FROM repomesh_issues.conversation_cards
        WHERE project_id = op.project_id AND source_id = op.id AND operation_id = op.id;
    IF n IS DISTINCT FROM 1 THEN
        RAISE EXCEPTION 'creation_aggregate_complete: conversation card missing for operation %', op.id
            USING ERRCODE = 'check_violation';
    END IF;
    SELECT count(*) INTO scope_count FROM repomesh_issues.issue_content_scope
        WHERE project_id = op.project_id AND issue_id = op.issue_id
          AND introduced_by_operation = op.id;
    IF scope_count < 1 THEN
        RAISE EXCEPTION 'creation_aggregate_complete: empty issue content scope for operation %', op.id
            USING ERRCODE = 'check_violation';
    END IF;
    SELECT count(*) INTO n FROM repomesh_issues.issue_repository_scope
        WHERE project_id = op.project_id AND issue_id = op.issue_id;
    IF n < scope_count THEN
        RAISE EXCEPTION 'creation_aggregate_complete: repository scope smaller than content scope for operation %', op.id
            USING ERRCODE = 'check_violation';
    END IF;
    SELECT count(*) INTO n FROM repomesh_issues.conversation_content_scope
        WHERE project_id = op.project_id AND conversation_id = op.conversation_id
          AND introduced_by_operation = op.id;
    IF n < scope_count THEN
        RAISE EXCEPTION 'creation_aggregate_complete: conversation scope smaller than issue scope for operation %', op.id
            USING ERRCODE = 'check_violation';
    END IF;
    SELECT count(*) INTO n FROM repomesh_issues.continuation_work
        WHERE project_id = op.project_id AND issue_id = op.issue_id
          AND cause_operation_id = op.id AND kind = 'issue_continue' AND state = 'blocked';
    IF n IS DISTINCT FROM 1 THEN
        RAISE EXCEPTION 'creation_aggregate_complete: blocked continuation work missing for operation %', op.id
            USING ERRCODE = 'check_violation';
    END IF;
    SELECT count(*) INTO n FROM repomesh_issues.issue_events
        WHERE issue_id = op.issue_id;
    IF n < 1 THEN
        RAISE EXCEPTION 'creation_aggregate_complete: no creation event for operation %', op.id
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

CREATE CONSTRAINT TRIGGER creation_aggregate_complete
AFTER INSERT OR UPDATE ON repomesh_issues.creation_operations
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION repomesh_issues.creation_aggregate_complete();

-- 2. issue_origin_consistent: the issue's creation identity must equal the
-- operation's view. Both sides queue the check on insert and identity change.
CREATE OR REPLACE FUNCTION repomesh_issues.issue_origin_consistent() RETURNS trigger AS $$
DECLARE
    expected text;
BEGIN
    SELECT id INTO expected FROM repomesh_issues.creation_operations
        WHERE project_id = NEW.project_id AND id = NEW.creation_operation_id;
    IF expected IS NULL THEN
        RAISE EXCEPTION 'issue_origin_consistent: creation operation % not found in project', NEW.creation_operation_id
            USING ERRCODE = 'foreign_key_violation';
    END IF;
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

CREATE CONSTRAINT TRIGGER issue_origin_consistent_issue
AFTER INSERT OR UPDATE OF creation_operation_id ON repomesh_issues.issues
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION repomesh_issues.issue_origin_consistent();

CREATE OR REPLACE FUNCTION repomesh_issues.issue_origin_consistent_operation() RETURNS trigger AS $$
DECLARE
    issue record;
    mismatch boolean;
BEGIN
    SELECT * INTO issue FROM repomesh_issues.issues
        WHERE project_id = NEW.project_id AND id = NEW.issue_id;
    IF NOT FOUND THEN RETURN NULL; END IF; -- issue not written yet; other side checks
    IF issue.creation_operation_id <> NEW.id
       OR issue.initial_configuration_revision IS DISTINCT FROM NEW.initial_configuration_revision THEN
        RAISE EXCEPTION 'issue_origin_consistent_operation: issue % does not originate from operation %', NEW.issue_id, NEW.id
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

CREATE CONSTRAINT TRIGGER issue_origin_consistent_operation
AFTER INSERT OR UPDATE OF issue_id, initial_configuration_revision ON repomesh_issues.creation_operations
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION repomesh_issues.issue_origin_consistent_operation();

-- 3. scope_complete: content scope containment. The issue content scope must
-- cover the work scope; the conversation content scope must cover the issue
-- content scope; both must be non-empty for a live aggregate; content rows
-- are append-only (deletion refused).
CREATE OR REPLACE FUNCTION repomesh_issues.scope_complete_issue() RETURNS trigger AS $$
DECLARE
    missing bigint;
    op record;
BEGIN
    SELECT removed_at, issue_id INTO op FROM repomesh_issues.creation_operations
        WHERE project_id = NEW.project_id AND id = NEW.creation_operation_id;
    IF NOT FOUND OR op.removed_at IS NOT NULL OR op.issue_id IS NULL THEN RETURN NULL; END IF;
    SELECT count(*) INTO missing FROM repomesh_issues.issue_content_scope ics
        WHERE ics.project_id = NEW.project_id AND ics.issue_id = NEW.id
          AND NOT EXISTS (
              SELECT 1 FROM repomesh_issues.issue_repository_scope irs
              WHERE irs.project_id = ics.project_id AND irs.issue_id = ics.issue_id
                AND irs.repository_id = ics.repository_id);
    IF missing > 0 THEN
        RAISE EXCEPTION 'scope_complete_issue: % content-scope rows without repository scope for issue %', missing, NEW.id
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

CREATE CONSTRAINT TRIGGER scope_complete_issue
AFTER INSERT OR UPDATE ON repomesh_issues.issues
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION repomesh_issues.scope_complete_issue();

CREATE OR REPLACE FUNCTION repomesh_issues.scope_complete_conversation() RETURNS trigger AS $$
DECLARE
    missing bigint;
    op record;
BEGIN
    SELECT removed_at, issue_id INTO op FROM repomesh_issues.creation_operations
        WHERE project_id = NEW.project_id AND id = NEW.created_by_operation_id;
    IF NOT FOUND OR op.removed_at IS NOT NULL OR op.issue_id IS NULL THEN RETURN NULL; END IF;
    SELECT count(*) INTO missing FROM repomesh_issues.conversation_content_scope ccs
        WHERE ccs.project_id = NEW.project_id AND ccs.conversation_id = NEW.id
          AND NOT EXISTS (
              SELECT 1 FROM repomesh_issues.issue_content_scope ics
              JOIN repomesh_issues.issues i ON i.project_id = ics.project_id AND i.id = ics.issue_id
              WHERE ics.project_id = NEW.project_id AND ics.introduced_by_operation = NEW.created_by_operation_id
                AND ics.repository_id = ccs.repository_id
                AND i.main_conversation_id = NEW.id);
    IF missing > 0 THEN
        RAISE EXCEPTION 'scope_complete_conversation: % conversation scope rows without issue scope for conversation %', missing, NEW.id
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

CREATE CONSTRAINT TRIGGER scope_complete_conversation
AFTER INSERT OR UPDATE ON repomesh_issues.conversations
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION repomesh_issues.scope_complete_conversation();

CREATE OR REPLACE FUNCTION repomesh_issues.scope_content_immutable() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'scope_content_immutable: issue and conversation content scope rows are append-only'
        USING ERRCODE = 'restrict_violation';
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER scope_content_immutable_issue
BEFORE DELETE ON repomesh_issues.issue_content_scope
FOR EACH ROW EXECUTE FUNCTION repomesh_issues.scope_content_immutable();

CREATE TRIGGER scope_content_immutable_conversation
BEFORE DELETE ON repomesh_issues.conversation_content_scope
FOR EACH ROW EXECUTE FUNCTION repomesh_issues.scope_content_immutable();

-- 4. immutable facts: configuration/provider identity on the issue, and the
-- creation identity on the operation, may never be UPDATEd or DELETEd. The
-- only sanctioned mutations are body cleanup (title/description redaction on
-- conversations) and operation removal (removed_at set, inputs + receipt
-- dropped). removed -> live resurrection is impossible because removed_at
-- updates on the operation are limited to the NULL -> timestamp transition
-- and the cleanup CHECK pins the inputs to NULL.
CREATE OR REPLACE FUNCTION repomesh_issues.immutable_issue_facts() RETURNS trigger AS $$
BEGIN
    IF NEW.project_id <> OLD.project_id OR NEW.id <> OLD.id
       OR NEW.number <> OLD.number OR NEW.title IS DISTINCT FROM OLD.title
       OR NEW.description IS DISTINCT FROM OLD.description
       OR NEW.criteria IS DISTINCT FROM OLD.criteria
       OR NEW.main_conversation_id IS DISTINCT FROM OLD.main_conversation_id
       OR NEW.main_changeset_id IS DISTINCT FROM OLD.main_changeset_id
       OR NEW.initial_configuration_revision IS DISTINCT FROM OLD.initial_configuration_revision
       OR NEW.creation_operation_id IS DISTINCT FROM OLD.creation_operation_id THEN
        RAISE EXCEPTION 'immutable_issue_facts: identity, initial configuration, and content facts are immutable on issues'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER immutable_issue_facts
BEFORE UPDATE ON repomesh_issues.issues
FOR EACH ROW EXECUTE FUNCTION repomesh_issues.immutable_issue_facts();

CREATE OR REPLACE FUNCTION repomesh_issues.immutable_provider_fact() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'immutable_provider_fact: issues rows can only be tombstoned via removed_at, never deleted'
        USING ERRCODE = 'restrict_violation';
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER immutable_provider_fact
BEFORE DELETE ON repomesh_issues.issues
FOR EACH ROW EXECUTE FUNCTION repomesh_issues.immutable_provider_fact();

CREATE OR REPLACE FUNCTION repomesh_issues.immutable_creation_identity() RETURNS trigger AS $$
BEGIN
    -- removed_at may only transition from NULL to a timestamp (cleanup); the
    -- cleanup CHECK pins canonical/exact/receipt to NULL, so once removed the
    -- row can never carry inputs again and the transition cannot be reversed.
    IF NEW.removed_at IS NULL AND OLD.removed_at IS NOT NULL THEN
        RAISE EXCEPTION 'immutable_creation_identity: removed operations cannot be resurrected'
            USING ERRCODE = 'check_violation';
    END IF;
    IF NEW.project_id <> OLD.project_id OR NEW.actor <> OLD.actor
       OR NEW.entry <> OLD.entry OR NEW.creation_id <> OLD.creation_id
       OR NEW.id IS DISTINCT FROM OLD.id
       OR NEW.schema_version <> OLD.schema_version
       OR NEW.issue_id IS DISTINCT FROM OLD.issue_id
       OR NEW.main_changeset_id IS DISTINCT FROM OLD.main_changeset_id
       OR NEW.conversation_id IS DISTINCT FROM OLD.conversation_id
       OR NEW.initial_configuration_revision IS DISTINCT FROM OLD.initial_configuration_revision THEN
        RAISE EXCEPTION 'immutable_creation_identity: identity and result columns on creation_operations are immutable'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER immutable_creation_identity
BEFORE UPDATE ON repomesh_issues.creation_operations
FOR EACH ROW EXECUTE FUNCTION repomesh_issues.immutable_creation_identity();

-- 5. creation_receipt_consistent: the receipt JSON projection must agree with
-- the authoritative structured columns, and receipt cleanup must accompany
-- operation cleanup.
CREATE OR REPLACE FUNCTION repomesh_issues.creation_receipt_consistent() RETURNS trigger AS $$
BEGIN
    IF NEW.receipt IS DISTINCT FROM OLD.receipt THEN
        IF NEW.receipt IS NOT NULL THEN
            IF NEW.receipt->>'issueId' IS DISTINCT FROM NEW.issue_id
               OR NEW.receipt->>'mainChangesetId' IS DISTINCT FROM NEW.main_changeset_id
               OR NEW.receipt->>'conversationId' IS DISTINCT FROM NEW.conversation_id
               OR NEW.receipt->>'initialConfigurationRevision' IS DISTINCT FROM NEW.initial_configuration_revision THEN
                RAISE EXCEPTION 'creation_receipt_consistent: receipt JSON projection disagrees with structured columns'
                    USING ERRCODE = 'check_violation';
            END IF;
        END IF;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER creation_receipt_consistent
BEFORE UPDATE OF receipt ON repomesh_issues.creation_operations
FOR EACH ROW EXECUTE FUNCTION repomesh_issues.creation_receipt_consistent();

-- 6. configuration_owner_and_binding_consistent: lives on
-- repomesh_projects.configuration_revisions (design lines 127-129). Both
-- profile bindings must be owned by the project owner; the secret reference
-- inside each fixed profile must equal the exact profile_versions metadata;
-- the materialized parameters must equal the bound execution_versions row.
CREATE OR REPLACE FUNCTION repomesh_projects.configuration_owner_and_binding_consistent() RETURNS trigger AS $$
DECLARE
    proj record;
    model_row record;
    execution_row record;
    model_secret record;
    execution_secret record;
    model_exec record;
BEGIN
    SELECT owner INTO proj FROM repomesh_projects.projects WHERE id = NEW.project_id;
    IF proj.owner IS NULL THEN
        RAISE EXCEPTION 'configuration_owner_and_binding_consistent: project % not found', NEW.project_id
            USING ERRCODE = 'foreign_key_violation';
    END IF;
    IF NEW.model_profile_id IS NOT NULL THEN
        SELECT owner INTO model_row FROM repomesh_projects.profiles
            WHERE kind = 'model' AND id = NEW.model_profile_id;
        IF model_row.owner IS DISTINCT FROM proj.owner THEN
            RAISE EXCEPTION 'configuration_owner_and_binding_consistent: model profile % is owned by %, project owner is %',
                NEW.model_profile_id, model_row.owner, proj.owner
                USING ERRCODE = 'check_violation';
        END IF;
        SELECT secret_version_id, secret_owner_kind, secret_owner_id, secret_purpose INTO model_secret
            FROM repomesh_projects.profile_versions
            WHERE kind = 'model' AND profile_id = NEW.model_profile_id AND version = NEW.model_profile_version;
        IF (NEW.fixed->'model'->'secret'->>'versionId') IS DISTINCT FROM model_secret.secret_version_id
           OR (NEW.fixed->'model'->'secret'->>'ownerKind') IS DISTINCT FROM model_secret.secret_owner_kind
           OR (NEW.fixed->'model'->'secret'->>'ownerId') IS DISTINCT FROM model_secret.secret_owner_id
           OR (NEW.fixed->'model'->'secret'->>'purpose') IS DISTINCT FROM model_secret.secret_purpose THEN
            RAISE EXCEPTION 'configuration_owner_and_binding_consistent: fixed model secret reference does not match profile version metadata'
                USING ERRCODE = 'check_violation';
        END IF;
    END IF;
    IF NEW.execution_profile_id IS NOT NULL THEN
        SELECT owner INTO execution_row FROM repomesh_projects.profiles
            WHERE kind = 'execution' AND id = NEW.execution_profile_id;
        IF execution_row.owner IS DISTINCT FROM proj.owner THEN
            RAISE EXCEPTION 'configuration_owner_and_binding_consistent: execution profile % is owned by %, project owner is %',
                NEW.execution_profile_id, execution_row.owner, proj.owner
                USING ERRCODE = 'check_violation';
        END IF;
        SELECT secret_version_id, secret_owner_kind, secret_owner_id, secret_purpose INTO execution_secret
            FROM repomesh_projects.profile_versions
            WHERE kind = 'execution' AND profile_id = NEW.execution_profile_id AND version = NEW.execution_profile_version;
        IF (NEW.fixed->'execution'->'secret'->>'versionId') IS DISTINCT FROM execution_secret.secret_version_id
           OR (NEW.fixed->'execution'->'secret'->>'ownerKind') IS DISTINCT FROM execution_secret.secret_owner_kind
           OR (NEW.fixed->'execution'->'secret'->>'ownerId') IS DISTINCT FROM execution_secret.secret_owner_id
           OR (NEW.fixed->'execution'->'secret'->>'purpose') IS DISTINCT FROM execution_secret.secret_purpose THEN
            RAISE EXCEPTION 'configuration_owner_and_binding_consistent: fixed execution secret reference does not match profile version metadata'
                USING ERRCODE = 'check_violation';
        END IF;
        SELECT worker_concurrency, verification_group_enabled INTO model_exec
            FROM repomesh_sources.execution_versions
            WHERE profile_id = NEW.execution_profile_id AND version = NEW.execution_profile_version AND complete;
        IF (NEW.fixed->>'workerConcurrency')::int IS DISTINCT FROM model_exec.worker_concurrency
           OR (NEW.fixed->>'verificationGroupEnabled')::boolean IS DISTINCT FROM model_exec.verification_group_enabled THEN
            RAISE EXCEPTION 'configuration_owner_and_binding_consistent: materialized parameters disagree with execution_versions'
                USING ERRCODE = 'check_violation';
        END IF;
    END IF;
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

CREATE CONSTRAINT TRIGGER configuration_owner_and_binding_consistent
AFTER INSERT ON repomesh_projects.configuration_revisions
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION repomesh_projects.configuration_owner_and_binding_consistent();
