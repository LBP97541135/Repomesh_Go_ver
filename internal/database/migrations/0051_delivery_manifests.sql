-- 0051_delivery_manifests.sql — 跨仓交付的**一致版本清单**（评委建议②）
--
-- 评委原话：企业需要明确一次交付包含哪些仓库提交、使用什么 Schema、测试结果对应
-- 哪套环境。建议在 PostgreSQL 数据层里关联需求、仓库版本集合、迁移版本、数据基线、
-- 分支与验证记录，并**保留失败尝试**（重跑不得覆盖原始证据），对重复事件与并发任务
-- 使用幂等标识与状态校验；展示时能从一次交付结果展开完整版本清单，定位失败发生在
-- 哪个仓库与数据接口，而不只是看到"任务全部变绿"。
--
-- 设计取舍：
--   · 清单是**一次交付的快照**（manifest），不是实时视图 —— 只有快照才能回答
--     "那次交付到底是哪些版本"。同一个 (plan, plan_version, idempotency_key) 只落
--     一份；换键重跑会落**新的一份**，旧的那份原样保留（失败尝试不丢）。
--   · 每个仓库一行 entry：代码侧（commit/branch/PR）、数据库侧（迁移版本、数据基线、
--     分支、provider、验证结论）、测试证据（test_evidence 的 kind/passed/summary），
--     以及**失败定位**（哪个阶段、哪条语句/哪个接口）。
--   · 拿不到的事实留空（不编）：例如没有数据库改动就没有迁移版本，没跑过分支验证
--     就是 validation_status='not_run'。

-- 排序用的时间戳：scm_commands 与 database_branch_validations 此前**都没有**
-- created_at，于是"这个仓库最近一次推送 / 最近一次分支验证"根本没法排序（清单要
-- 取最近一次）。老行统一补 now()：它们的真实时刻已经不可考，不编。
ALTER TABLE public.scm_commands ADD COLUMN IF NOT EXISTS created_at timestamptz NOT NULL DEFAULT now();
ALTER TABLE public.database_branch_validations ADD COLUMN IF NOT EXISTS created_at timestamptz NOT NULL DEFAULT now();

CREATE TABLE IF NOT EXISTS public.delivery_manifests (
    id               uuid PRIMARY KEY,
    project_id       uuid NOT NULL,
    issue_id         text NOT NULL,
    plan_id          uuid,
    plan_version     text NOT NULL DEFAULT '',
    requirement_text text NOT NULL DEFAULT '',
    -- consistent = 每个仓库的代码/数据库/测试三面都有结论且没失败；
    -- inconsistent = 有仓库缺结论或失败；failed = 建清单本身失败。
    status           text NOT NULL CHECK (status IN ('consistent', 'inconsistent', 'failed')),
    failure_summary  text NOT NULL DEFAULT '',
    idempotency_key  text NOT NULL UNIQUE,
    request_hash     text NOT NULL DEFAULT '',
    created_by       text NOT NULL DEFAULT '',
    created_at       timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS delivery_manifests_issue
    ON public.delivery_manifests (project_id, issue_id, created_at DESC);

CREATE TABLE IF NOT EXISTS public.delivery_manifest_entries (
    id                uuid PRIMARY KEY,
    manifest_id       uuid NOT NULL REFERENCES public.delivery_manifests(id) ON DELETE CASCADE,
    repository_id     text NOT NULL,
    repository_name   text NOT NULL DEFAULT '',
    -- 代码侧
    commit_sha        text NOT NULL DEFAULT '',
    branch_ref        text NOT NULL DEFAULT '',
    pull_request_url  text NOT NULL DEFAULT '',
    -- 数据库侧（没有数据库改动时全为空，validation_status='not_run'）
    migrations        jsonb NOT NULL DEFAULT '[]'::jsonb,
    database_baseline text NOT NULL DEFAULT '',
    database_branch   text NOT NULL DEFAULT '',
    database_provider text NOT NULL DEFAULT '',
    validation_status text NOT NULL DEFAULT 'not_run',
    -- 失败定位：阶段 + 明细（哪条迁移语句、哪个接口）
    failure_stage     text NOT NULL DEFAULT '',
    failure_detail    text NOT NULL DEFAULT '',
    -- 测试证据（test_evidence 的 kind/passed/summary 摘录）
    test_evidence     jsonb NOT NULL DEFAULT '[]'::jsonb,
    created_at        timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS delivery_manifest_entries_manifest
    ON public.delivery_manifest_entries (manifest_id, repository_name);

-- ── C③ 环境治理与回收（评委建议③）────────────────────────────────────────
-- 分支的**创建/使用/清理**三态此前只有 cleanup_pending 一个布尔，重试与失败原因
-- 没地方记：清理失败过几次、最后一次为什么失败、什么时候真正回收掉的 —— 都没留。
-- 服务重启后也就无从判断"这条该继续清还是已经清完了"。
ALTER TABLE public.database_branch_validations
  ADD COLUMN IF NOT EXISTS cleanup_attempts integer NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS last_cleanup_error text NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS reclaimed_at timestamptz;

-- 回收扫描的读面：待清理的行（含卡在 provisioning 的僵尸行）按时间取。
CREATE INDEX IF NOT EXISTS idx_dbv_cleanup_pending
  ON public.database_branch_validations (cleanup_pending, created_at)
  WHERE cleanup_pending;
