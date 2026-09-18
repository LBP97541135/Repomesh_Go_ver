-- B09: message persistence and clarification state machine (repomesh_messages).
-- Design source: docs/current/backend-message-clarification-design.md §3.
-- Messages attach to repomesh_issues.conversations (B06); one submission per
-- (project, conversation, actor, entry, submissionId); sequence allocated under
-- the conversation row lock.

CREATE SCHEMA IF NOT EXISTS repomesh_messages;

-- 1. conversation_messages -----------------------------------------------------
CREATE TABLE repomesh_messages.conversation_messages (
    id text PRIMARY KEY,
    project_id text NOT NULL,
    conversation_id text NOT NULL,
    sequence bigint NOT NULL CHECK (sequence > 0),
    author_kind text NOT NULL CHECK (author_kind IN ('user', 'service')),
    actor_id text NOT NULL,
    body text NOT NULL,
    reply_to_clarification text,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    removed_at timestamptz,
    CONSTRAINT conversation_messages_seq UNIQUE (project_id, conversation_id, sequence),
    CONSTRAINT conversation_messages_conv_fk FOREIGN KEY (project_id, conversation_id)
        REFERENCES repomesh_issues.conversations(project_id, id)
);

-- 2. message_submissions (idempotency ledger) ----------------------------------
CREATE TABLE repomesh_messages.message_submissions (
    project_id text NOT NULL,
    conversation_id text NOT NULL,
    actor text NOT NULL,
    entry text NOT NULL CHECK (entry = 'conversation_message'),
    submission_id text NOT NULL,
    id text NOT NULL UNIQUE,
    canonical_input bytea NOT NULL,
    exact_input bytea NOT NULL,
    input_digest bytea NOT NULL,
    message_id text,
    receipt jsonb,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    removed_at timestamptz,
    CONSTRAINT message_submissions_pk PRIMARY KEY (project_id, conversation_id, actor, entry, submission_id),
    CONSTRAINT message_submissions_cleanup CHECK (
        (removed_at IS NULL) OR (canonical_input IS NULL AND exact_input IS NULL AND receipt IS NULL)
    )
);

-- 3. message_processing_entries ------------------------------------------------
CREATE TABLE repomesh_messages.message_processing_entries (
    message_id text PRIMARY KEY,
    project_id text NOT NULL,
    conversation_id text NOT NULL,
    kind text NOT NULL CHECK (kind = 'interpret_message'),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT message_processing_entries_message_fk FOREIGN KEY (message_id)
        REFERENCES repomesh_messages.conversation_messages(id)
);

-- 4. logical_work_requests -----------------------------------------------------
CREATE TABLE repomesh_messages.logical_work_requests (
    id text PRIMARY KEY,
    project_id text NOT NULL,
    conversation_id text NOT NULL,
    source_message_id text NOT NULL,
    source_revision bigint NOT NULL CHECK (source_revision > 0),
    actor text NOT NULL,
    source_unit text NOT NULL CHECK (source_unit = 'whole_message_single_request'),
    state text NOT NULL CHECK (state IN ('pending','awaiting_clarification','answer_pending','resolved','needs_decomposition','invalidated')),
    revision bigint NOT NULL CHECK (revision > 0),
    current_clarification_id text,
    resolution_id text,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);

-- 5. clarifications ------------------------------------------------------------
CREATE TABLE repomesh_messages.clarifications (
    id text PRIMARY KEY,
    project_id text NOT NULL,
    logical_request_id text NOT NULL,
    question_message_id text NOT NULL,
    state text NOT NULL CHECK (state IN ('open','answer_saved','superseded','invalidated')),
    revision bigint NOT NULL CHECK (revision > 0),
    superseded_by text,
    invalidation_reason text,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT clarifications_request_fk FOREIGN KEY (logical_request_id)
        REFERENCES repomesh_messages.logical_work_requests(id)
);

-- one open or answer_saved question per request at any moment
CREATE UNIQUE INDEX clarifications_active_per_request
    ON repomesh_messages.clarifications(logical_request_id)
    WHERE state IN ('open', 'answer_saved');

-- 6. clarification_answers -----------------------------------------------------
CREATE TABLE repomesh_messages.clarification_answers (
    clarification_id text PRIMARY KEY,
    answer_message_id text NOT NULL UNIQUE,
    answered_revision bigint NOT NULL CHECK (answered_revision > 0),
    actor text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT clarification_answers_clarification_fk FOREIGN KEY (clarification_id)
        REFERENCES repomesh_messages.clarifications(id)
);

-- 7. target_resolutions (one final interpretation per request) ------------------
CREATE TABLE repomesh_messages.target_resolutions (
    logical_request_id text PRIMARY KEY,
    resolution_id text NOT NULL UNIQUE,
    input_kind text NOT NULL CHECK (input_kind IN ('root_message','clarification_answer')),
    outcome text NOT NULL CHECK (outcome IN ('issue_target','no_work')),
    issue_id text,
    evidence jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT target_resolutions_issue CHECK (
        (outcome = 'issue_target' AND issue_id IS NOT NULL) OR (outcome = 'no_work' AND issue_id IS NULL)
    )
);

ALTER TABLE repomesh_messages.target_resolutions
    ADD CONSTRAINT target_resolutions_issue_fk
    FOREIGN KEY (issue_id)
    REFERENCES repomesh_issues.issues(id)
    DEFERRABLE INITIALLY DEFERRED;

-- 8. control_operations (command slots) -----------------------------------------
CREATE TABLE repomesh_messages.control_operations (
    command_slot_id text PRIMARY KEY,
    project_id text NOT NULL,
    action text NOT NULL,
    schema_version integer NOT NULL CHECK (schema_version = 1),
    canonical_input bytea NOT NULL,
    result jsonb,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    removed_at timestamptz
);

-- 9. delivery_observations -------------------------------------------------------
CREATE TABLE repomesh_messages.delivery_observations (
    message_id text NOT NULL,
    external_operation text NOT NULL,
    state text NOT NULL CHECK (state IN ('possible_send','reconciling','delivered')),
    observed_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT delivery_observations_pk PRIMARY KEY (message_id, external_operation)
);
