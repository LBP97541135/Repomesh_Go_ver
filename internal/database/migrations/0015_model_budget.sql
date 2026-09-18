-- 0015: B05 模型预算与政策（ASTRA B05, docs/development/2026-09-13-b04-b06-design-01/migration-design.md）
-- 政策版本表（projects/sources）+ modelbudget 窗口账本 + 来源导入 schema2 扩展。
-- 本迁移不放 test_reservations：它复合外键到 models.tests(actor,test_id)，tests 表在 0016 建。

CREATE SCHEMA repomesh_modelbudget;

-- repomesh_projects.request_policy_versions：运行请求政策（B05 §9）
CREATE TABLE repomesh_projects.request_policy_versions (
    id text NOT NULL CHECK (length(id) BETWEEN 1 AND 128),
    version text NOT NULL CHECK (length(version) BETWEEN 1 AND 128),
    scope text NOT NULL CHECK (scope IN ('actor_model_test', 'project_model_runtime')),
    daily_limit bigint NOT NULL CHECK (daily_limit BETWEEN 1 AND 2147483647),
    max_unresolved bigint NOT NULL CHECK (max_unresolved >= 1),
    enabled boolean NOT NULL,
    PRIMARY KEY (id, version)
);

-- repomesh_projects.time_policy_versions：时限政策（B05 §9）
CREATE TABLE repomesh_projects.time_policy_versions (
    id text NOT NULL CHECK (length(id) BETWEEN 1 AND 128),
    version text NOT NULL CHECK (length(version) BETWEEN 1 AND 128),
    scope text NOT NULL CHECK (scope = 'project_model_runtime'),
    model_request_seconds integer NOT NULL CHECK (model_request_seconds BETWEEN 5 AND 120),
    worker_attempt_seconds integer NOT NULL CHECK (worker_attempt_seconds BETWEEN 60 AND 86400),
    PRIMARY KEY (id, version)
);

CREATE TRIGGER request_policy_versions_immutable
BEFORE UPDATE OR DELETE ON repomesh_projects.request_policy_versions
FOR EACH ROW EXECUTE FUNCTION repomesh_projects.reject_immutable_change();

CREATE TRIGGER time_policy_versions_immutable
BEFORE UPDATE OR DELETE ON repomesh_projects.time_policy_versions
FOR EACH ROW EXECUTE FUNCTION repomesh_projects.reject_immutable_change();

-- repomesh_sources.egress_policy_versions：来源导入附带的出站白名单（不可变）
CREATE TABLE repomesh_sources.egress_policy_versions (
    id text NOT NULL CHECK (length(id) BETWEEN 1 AND 128),
    version text NOT NULL CHECK (length(version) BETWEEN 1 AND 128),
    approved_base_urls text[] NOT NULL,
    allowed_port integer NOT NULL CHECK (allowed_port = 443),
    allow_private_addresses boolean NOT NULL,
    follow_redirects boolean NOT NULL,
    PRIMARY KEY (id, version),
    -- 至少一项白名单
    CHECK (array_length(approved_base_urls, 1) IS NOT NULL)
);

-- 白名单规范化无重项（CHECK 不能含子查询，用行级触发器校验）
CREATE FUNCTION repomesh_sources.egress_urls_distinct() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF (SELECT count(DISTINCT e) FROM unnest(NEW.approved_base_urls) AS e)
        <> array_length(NEW.approved_base_urls, 1) THEN
        RAISE EXCEPTION 'egress approved_base_urls must not contain duplicates';
    END IF;
    RETURN NULL;
END;
$$;

CREATE TRIGGER egress_policy_urls_distinct
BEFORE INSERT ON repomesh_sources.egress_policy_versions
FOR EACH ROW EXECUTE FUNCTION repomesh_sources.egress_urls_distinct();

CREATE FUNCTION repomesh_sources.reject_immutable_change() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'immutable RepoMesh source version';
END;
$$;

-- egress 政策版本不可变
CREATE TRIGGER egress_policy_versions_immutable
BEFORE UPDATE OR DELETE ON repomesh_sources.egress_policy_versions
FOR EACH ROW EXECUTE FUNCTION repomesh_sources.reject_immutable_change();

-- repomesh_sources.test_bindings：每个 owner 一行，绑定三个确切政策版本
CREATE TABLE repomesh_sources.test_bindings (
    owner text NOT NULL REFERENCES repomesh_access.accounts(id),
    budget_policy_id text NOT NULL,
    budget_policy_version text NOT NULL,
    time_limit_policy_id text NOT NULL,
    time_limit_policy_version text NOT NULL,
    egress_policy_id text NOT NULL,
    egress_policy_version text NOT NULL,
    combination_revision text NOT NULL CHECK (combination_revision ~ '^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'),
    PRIMARY KEY (owner),
    FOREIGN KEY (budget_policy_id, budget_policy_version)
        REFERENCES repomesh_projects.request_policy_versions(id, version),
    FOREIGN KEY (time_limit_policy_id, time_limit_policy_version)
        REFERENCES repomesh_projects.time_policy_versions(id, version),
    FOREIGN KEY (egress_policy_id, egress_policy_version)
        REFERENCES repomesh_sources.egress_policy_versions(id, version)
);

-- modelbudget.windows：每 scope 每 UTC 日一行（migration-design.md 精确 DDL 形状）
CREATE TABLE repomesh_modelbudget.windows (
    scope_kind text NOT NULL
      CHECK (scope_kind IN ('actor_model_test', 'project_model_runtime')),
    actor_scope_id text REFERENCES repomesh_access.accounts(id),
    project_scope_id text REFERENCES repomesh_projects.projects(id),
    scope_id text GENERATED ALWAYS AS
      (COALESCE(actor_scope_id, project_scope_id)) STORED,
    CHECK (
      (scope_kind = 'actor_model_test' AND actor_scope_id IS NOT NULL AND project_scope_id IS NULL)
      OR
      (scope_kind = 'project_model_runtime' AND actor_scope_id IS NULL AND project_scope_id IS NOT NULL)
    ),
    start_utc timestamptz NOT NULL,
    end_utc timestamptz NOT NULL,
    CHECK (start_utc = date_trunc('day', start_utc AT TIME ZONE 'UTC') AT TIME ZONE 'UTC'),
    CHECK (end_utc = start_utc + interval '1 day'),
    daily_limit bigint NOT NULL CHECK (daily_limit > 0),
    reserved bigint NOT NULL DEFAULT 0 CHECK (reserved >= 0),
    consumed bigint NOT NULL DEFAULT 0 CHECK (consumed >= 0),
    revision text NOT NULL CHECK (revision ~ '^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'),
    PRIMARY KEY (scope_kind, scope_id, start_utc),
    CHECK (reserved <= daily_limit AND consumed <= daily_limit)
);

-- 同窗口 limit 只能减少
CREATE FUNCTION repomesh_modelbudget.window_limit_only_decreases() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.daily_limit > OLD.daily_limit THEN
        RAISE EXCEPTION 'model budget window limit can only decrease';
    END IF;
    RETURN NULL;
END;
$$;

CREATE TRIGGER windows_limit_only_decreases
BEFORE UPDATE ON repomesh_modelbudget.windows
FOR EACH ROW EXECUTE FUNCTION repomesh_modelbudget.window_limit_only_decreases();

-- 来源导入 schema2：放宽 imports.schema_version 到 1..2，execution_versions 增加 v2 完整列
ALTER TABLE repomesh_sources.imports
    DROP CONSTRAINT imports_schema_version_check,
    ADD CONSTRAINT imports_schema_version_check CHECK (schema_version IN (1, 2));

-- v2 扩展范围按 migration-design.md：只有预算/时限 id+version 复合 FK 及完整标志。
-- egress 不进 execution_versions：它挂在 test_bindings（owner 维度），由 0016 的
-- models.test_bindings 接线，见 0015 顶部的 egress_policy_versions。
ALTER TABLE repomesh_sources.execution_versions
    ADD COLUMN budget_policy_id text,
    ADD COLUMN budget_policy_version text,
    ADD COLUMN time_limit_policy_id text,
    ADD COLUMN time_limit_policy_version text,
    ADD COLUMN complete boolean NOT NULL DEFAULT false;

ALTER TABLE repomesh_sources.execution_versions
    ADD CONSTRAINT execution_v2_policies_complete CHECK (
        (complete = false AND budget_policy_id IS NULL AND budget_policy_version IS NULL
            AND time_limit_policy_id IS NULL AND time_limit_policy_version IS NULL)
        OR
        (complete = true AND budget_policy_id IS NOT NULL AND budget_policy_version IS NOT NULL
            AND time_limit_policy_id IS NOT NULL AND time_limit_policy_version IS NOT NULL)
    );

ALTER TABLE repomesh_sources.execution_versions
    ADD CONSTRAINT execution_v2_budget_fk
    FOREIGN KEY (budget_policy_id, budget_policy_version)
        REFERENCES repomesh_projects.request_policy_versions(id, version),
    ADD CONSTRAINT execution_v2_time_limit_fk
    FOREIGN KEY (time_limit_policy_id, time_limit_policy_version)
        REFERENCES repomesh_projects.time_policy_versions(id, version);
