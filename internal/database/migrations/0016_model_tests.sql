-- 0016: B05 模型测试与应用（ASTRA B05, docs/development/2026-09-13-b04-b06-design-01/migration-design.md）
-- reservation 状态守卫：状态只能 reserved→consumed 或 reserved→released，
-- consumed 不可转 released，事实列一旦落定不可回改。
CREATE FUNCTION repomesh_modelbudget.reservation_state_guard() RETURNS trigger
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
    RETURN NULL;
END;
$$;

-- repomesh_models.previews：test/apply 两种预览，随机 id，不可变输入
CREATE TABLE repomesh_models.previews (
    id text PRIMARY KEY CHECK (id ~ '^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'),
    kind text NOT NULL CHECK (kind IN ('test', 'apply')),
    actor text NOT NULL REFERENCES repomesh_access.accounts(id),
    provider_id text NOT NULL,
    provider_revision text NOT NULL,
    model_row_id text NOT NULL,
    max_output_tokens bigint NOT NULL CHECK (max_output_tokens BETWEEN 1 AND 2147483647),
    budget_policy_id text NOT NULL,
    budget_policy_version text NOT NULL,
    time_limit_policy_id text NOT NULL,
    time_limit_policy_version text NOT NULL,
    egress_policy_id text NOT NULL,
    egress_policy_version text NOT NULL,
    project_id text,
    project_revision text,
    configuration_snapshot jsonb,
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    consumed_by_operation text,
    consumed_at timestamptz,
    FOREIGN KEY (provider_id, provider_revision, model_row_id)
        REFERENCES repomesh_models.model_snapshots(provider_id, provider_revision, row_id),
    FOREIGN KEY (budget_policy_id, budget_policy_version)
        REFERENCES repomesh_projects.request_policy_versions(id, version),
    FOREIGN KEY (time_limit_policy_id, time_limit_policy_version)
        REFERENCES repomesh_projects.time_policy_versions(id, version),
    FOREIGN KEY (egress_policy_id, egress_policy_version)
        REFERENCES repomesh_sources.egress_policy_versions(id, version),
    CHECK ((kind = 'apply') = (project_id IS NOT NULL)),
    CHECK ((project_id IS NULL) = (project_revision IS NULL)),
    CHECK ((project_id IS NULL) = (configuration_snapshot IS NULL))
);

-- consumed_by 只能空→一个 operation；消费墓碑永不删除
CREATE FUNCTION repomesh_models.preview_consume_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF OLD.consumed_by_operation IS NOT NULL AND NEW.consumed_by_operation IS DISTINCT FROM OLD.consumed_by_operation THEN
        RAISE EXCEPTION 'preview consumption is final';
    END IF;
    IF OLD.consumed_by_operation IS NULL AND NEW.consumed_by_operation IS NOT NULL AND NEW.consumed_at IS NULL THEN
        RAISE EXCEPTION 'preview consume requires consumed_at';
    END IF;
    RETURN NULL;
END;
$$;

CREATE TRIGGER previews_consume_guard
BEFORE UPDATE ON repomesh_models.previews
FOR EACH ROW EXECUTE FUNCTION repomesh_models.preview_consume_guard();

-- (actor,id) 组合唯一索引：tests/application_operations 复合 FK 的引用目标
CREATE UNIQUE INDEX previews_actor_id
ON repomesh_models.previews(actor, id);

-- repomesh_models.tests：登记后的固定单模型测试
CREATE TABLE repomesh_models.tests (
    actor text NOT NULL REFERENCES repomesh_access.accounts(id),
    test_id text NOT NULL CHECK (length(test_id) BETWEEN 1 AND 128),
    provider_id text NOT NULL,
    provider_revision text NOT NULL,
    model_row_id text NOT NULL,
    max_output_tokens bigint NOT NULL CHECK (max_output_tokens BETWEEN 1 AND 2147483647),
    budget_policy_id text NOT NULL,
    budget_policy_version text NOT NULL,
    time_limit_policy_id text NOT NULL,
    time_limit_policy_version text NOT NULL,
    egress_policy_id text NOT NULL,
    egress_policy_version text NOT NULL,
    preview_id text NOT NULL,
    canonical_input jsonb NOT NULL CHECK (jsonb_typeof(canonical_input) = 'object'),
    state text NOT NULL CHECK (state IN ('queued', 'running', 'passed', 'failed', 'unknown', 'rejected')),
    result_revision bigint NOT NULL DEFAULT 1 CHECK (result_revision >= 1),
    recovery text NOT NULL CHECK (recovery IN ('none', 'open', 'closed')),
    accepted_at timestamptz NOT NULL,
    observed_at timestamptz,
    result_code text,
    removed_at timestamptz,
    PRIMARY KEY (actor, test_id),
    FOREIGN KEY (provider_id, provider_revision, model_row_id)
        REFERENCES repomesh_models.model_snapshots(provider_id, provider_revision, row_id),
    FOREIGN KEY (budget_policy_id, budget_policy_version)
        REFERENCES repomesh_projects.request_policy_versions(id, version),
    FOREIGN KEY (time_limit_policy_id, time_limit_policy_version)
        REFERENCES repomesh_projects.time_policy_versions(id, version),
    FOREIGN KEY (egress_policy_id, egress_policy_version)
        REFERENCES repomesh_sources.egress_policy_versions(id, version),
    FOREIGN KEY (actor, preview_id) REFERENCES repomesh_models.previews(actor, id)
);

-- 身份与 accepted_at 不可改；preview 消费唯一（一 preview 至多一 test）
CREATE UNIQUE INDEX tests_preview_unique
ON repomesh_models.tests(actor, preview_id);

CREATE FUNCTION repomesh_models.tests_state_guard() RETURNS trigger
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
    RETURN NULL;
END;
$$;

CREATE TRIGGER tests_identity_immutable
BEFORE UPDATE ON repomesh_models.tests
FOR EACH ROW EXECUTE FUNCTION repomesh_models.tests_state_guard();

-- modelbudget.test_reservations + repomesh_models 9 张表：
-- previews / tests / test_handler_leases / test_dispatch / test_observations /
-- actor_outstanding_tests / unknown_test_closures / application_operations。

-- modelbudget.test_reservations：每 actor 每笔测试一个计数预留
CREATE TABLE repomesh_modelbudget.test_reservations (
    actor text NOT NULL,
    test_id text NOT NULL CHECK (length(test_id) BETWEEN 1 AND 128),
    scope_kind text NOT NULL CHECK (scope_kind = 'actor_model_test'),
    scope_id text NOT NULL,
    start_utc timestamptz NOT NULL,
    budget_policy_id text NOT NULL,
    budget_policy_version text NOT NULL,
    amount bigint NOT NULL CHECK (amount = 1),
    state text NOT NULL CHECK (state IN ('reserved', 'consumed', 'released')),
    reserved_at timestamptz NOT NULL,
    settled_at timestamptz,
    PRIMARY KEY (actor, test_id),
    FOREIGN KEY (actor, test_id) REFERENCES repomesh_models.tests(actor, test_id),
    FOREIGN KEY (scope_kind, scope_id, start_utc)
        REFERENCES repomesh_modelbudget.windows(scope_kind, scope_id, start_utc),
    FOREIGN KEY (budget_policy_id, budget_policy_version)
        REFERENCES repomesh_projects.request_policy_versions(id, version),
    CHECK ((state = 'reserved') = (settled_at IS NULL))
);

CREATE TRIGGER test_reservations_immutable
BEFORE UPDATE ON repomesh_modelbudget.test_reservations
FOR EACH ROW EXECUTE FUNCTION repomesh_modelbudget.reservation_state_guard();

-- repomesh_models.test_handler_leases：测试处理器租约（coordinator 写，Web 读）
CREATE TABLE repomesh_models.test_handler_leases (
    instance_id text PRIMARY KEY CHECK (length(instance_id) BETWEEN 1 AND 128),
    protocol_version text NOT NULL CHECK (length(protocol_version) BETWEEN 1 AND 64),
    last_seen timestamptz NOT NULL,
    lease_until timestamptz NOT NULL,
    retired_at timestamptz,
    CHECK (lease_until > last_seen),
    CHECK (retired_at IS NULL OR retired_at >= last_seen)
);

-- repomesh_models.test_dispatch：每测试一行发送责任（许可协议核心）
CREATE TABLE repomesh_models.test_dispatch (
    actor text NOT NULL,
    test_id text NOT NULL,
    external_operation_id text NOT NULL CHECK (length(external_operation_id) BETWEEN 1 AND 128),
    send_permit_id text NOT NULL UNIQUE CHECK (length(send_permit_id) BETWEEN 1 AND 128),
    may_have_sent_at timestamptz,
    generation bigint NOT NULL DEFAULT 1 CHECK (generation >= 1),
    lease_until timestamptz,
    sender_instance_id text,
    credential_capability_id text NOT NULL,
    credential_capability_version text NOT NULL,
    capability_revoked_at timestamptz,
    revocation_evidence_digest text,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (actor, test_id),
    UNIQUE (external_operation_id),
    FOREIGN KEY (actor, test_id) REFERENCES repomesh_models.tests(actor, test_id),
    FOREIGN KEY (sender_instance_id) REFERENCES repomesh_models.test_handler_leases(instance_id),
    CHECK ((lease_until IS NULL) = (sender_instance_id IS NULL)),
    CHECK ((capability_revoked_at IS NULL) = (revocation_evidence_digest IS NULL))
);

-- may_have_sent 只能空→非空；能力撤销只能空→固定事实
CREATE FUNCTION repomesh_models.dispatch_fact_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF OLD.may_have_sent_at IS NULL AND NEW.may_have_sent_at IS NULL
        AND NEW.may_have_sent_at IS NOT NULL THEN
        RAISE EXCEPTION 'unexpected';
    END IF;
    IF OLD.may_have_sent_at IS NOT NULL AND NEW.may_have_sent_at IS DISTINCT FROM OLD.may_have_sent_at THEN
        RAISE EXCEPTION 'may_have_sent_at is final once set';
    END IF;
    IF OLD.capability_revoked_at IS NOT NULL AND
        (NEW.capability_revoked_at IS DISTINCT FROM OLD.capability_revoked_at
         OR NEW.revocation_evidence_digest IS DISTINCT FROM OLD.revocation_evidence_digest) THEN
        RAISE EXCEPTION 'capability revocation is final';
    END IF;
    RETURN NULL;
END;
$$;

CREATE TRIGGER dispatch_fact_guard
BEFORE UPDATE ON repomesh_models.test_dispatch
FOR EACH ROW EXECUTE FUNCTION repomesh_models.dispatch_fact_guard();

-- (actor,test_id,external_operation_id) 组合唯一：observations/closures 复合 FK 的引用目标
CREATE UNIQUE INDEX dispatch_observation_key
ON repomesh_models.test_dispatch(actor, test_id, external_operation_id);

-- repomesh_models.test_observations：append-only 观察证据
CREATE TABLE repomesh_models.test_observations (
    evidence_id text PRIMARY KEY CHECK (length(evidence_id) BETWEEN 1 AND 128),
    actor text NOT NULL,
    test_id text NOT NULL,
    external_operation_id text NOT NULL,
    started_at timestamptz,
    observed_at timestamptz NOT NULL,
    code text NOT NULL,
    latency_ms bigint,
    input_tokens bigint CHECK (input_tokens >= 0),
    output_tokens bigint CHECK (output_tokens >= 0),
    generation bigint NOT NULL DEFAULT 1 CHECK (generation >= 1),
    recorded_by text NOT NULL,
    FOREIGN KEY (actor, test_id, external_operation_id)
        REFERENCES repomesh_models.test_dispatch(actor, test_id, external_operation_id),
    CHECK (latency_ms IS NULL OR latency_ms >= 0),
    CHECK ((input_tokens IS NULL) = (output_tokens IS NULL))
);

CREATE FUNCTION repomesh_models.reject_observation_change() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'test observations are append-only';
END;
$$;

CREATE TRIGGER test_observations_append_only
BEFORE UPDATE OR DELETE ON repomesh_models.test_observations
FOR EACH ROW EXECUTE FUNCTION repomesh_models.reject_observation_change();

-- repomesh_models.actor_outstanding_tests：每 actor 的未核清测试责任
CREATE TABLE repomesh_models.actor_outstanding_tests (
    actor text PRIMARY KEY REFERENCES repomesh_access.accounts(id),
    test_id text NOT NULL,
    opened_reason text NOT NULL,
    opened_at timestamptz NOT NULL,
    FOREIGN KEY (actor, test_id) REFERENCES repomesh_models.tests(actor, test_id)
);

-- repomesh_models.unknown_test_closures：unknown 带外关闭回执
CREATE TABLE repomesh_models.unknown_test_closures (
    deployment_identity text NOT NULL CHECK (length(deployment_identity) BETWEEN 1 AND 128),
    close_key text NOT NULL CHECK (length(close_key) BETWEEN 1 AND 128),
    actor text NOT NULL,
    test_id text NOT NULL,
    external_operation_id text NOT NULL,
    send_permit_id text NOT NULL,
    sender_instance_id text NOT NULL,
    credential_capability_id text NOT NULL,
    credential_capability_version text NOT NULL,
    schema_version integer NOT NULL CHECK (schema_version = 1),
    sender_exit_ref text NOT NULL,
    sender_exit_digest text NOT NULL CHECK (sender_exit_digest ~ '^[0-9a-f]{64}$'),
    capability_revocation_ref text NOT NULL,
    capability_revocation_digest text NOT NULL CHECK (capability_revocation_digest ~ '^[0-9a-f]{64}$'),
    operator_principal text NOT NULL,
    closed_at timestamptz NOT NULL,
    PRIMARY KEY (deployment_identity, close_key),
    UNIQUE (actor, test_id),
    FOREIGN KEY (actor, test_id) REFERENCES repomesh_models.tests(actor, test_id),
    FOREIGN KEY (actor, test_id, external_operation_id)
        REFERENCES repomesh_models.test_dispatch(actor, test_id, external_operation_id),
    FOREIGN KEY (sender_instance_id) REFERENCES repomesh_models.test_handler_leases(instance_id)
);

CREATE FUNCTION repomesh_models.reject_closure_change() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'unknown test closures are immutable';
END;
$$;

CREATE TRIGGER unknown_test_closures_immutable
BEFORE UPDATE OR DELETE ON repomesh_models.unknown_test_closures
FOR EACH ROW EXECUTE FUNCTION repomesh_models.reject_closure_change();

-- repomesh_models.application_operations：专用模型应用
CREATE TABLE repomesh_models.application_operations (
    project_id text NOT NULL,
    actor text NOT NULL REFERENCES repomesh_access.accounts(id),
    application_id text NOT NULL CHECK (length(application_id) BETWEEN 1 AND 128),
    request_id text NOT NULL CHECK (length(request_id) BETWEEN 1 AND 128),
    preview_id text NOT NULL,
    project_revision_before text NOT NULL,
    project_revision_after text,
    candidate_snapshot jsonb NOT NULL CHECK (jsonb_typeof(candidate_snapshot) = 'object'),
    outcome text NOT NULL CHECK (outcome IN ('committed', 'rejected')),
    rejection_code text,
    retained_execution_profile_id text,
    retained_execution_profile_version text,
    committed_at timestamptz NOT NULL,
    removed_at timestamptz,
    PRIMARY KEY (project_id, actor, application_id),
    FOREIGN KEY (project_id, project_revision_before)
        REFERENCES repomesh_projects.configuration_revisions(project_id, revision),
    FOREIGN KEY (project_id) REFERENCES repomesh_projects.projects(id),
    FOREIGN KEY (actor, preview_id)
        REFERENCES repomesh_models.previews(actor, id),
    CHECK ((outcome = 'committed') = (project_revision_after IS NOT NULL)),
    CHECK ((outcome = 'rejected') = (rejection_code IS NOT NULL))
);

-- application preview 消费唯一：一个 preview 至多一个 application operation
CREATE UNIQUE INDEX application_operations_preview_unique
ON repomesh_models.application_operations(project_id, actor, preview_id);

-- operation 回执不可变（removed_at 除外）
CREATE FUNCTION repomesh_models.application_receipt_guard() RETURNS trigger
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
    RETURN NULL;
END;
$$;

CREATE TRIGGER application_receipt_guard
BEFORE UPDATE ON repomesh_models.application_operations
FOR EACH ROW EXECUTE FUNCTION repomesh_models.application_receipt_guard();

-- unknown 运维关闭专用角色（coordinator 本地 CLI 使用）
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'repomesh_unknown_maintainer') THEN
        CREATE ROLE repomesh_unknown_maintainer NOLOGIN;
    END IF;
END
$$;
GRANT repomesh_unknown_maintainer TO CURRENT_USER;
