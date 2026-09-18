-- 0012_delivery_validation.sql — Go 版重建数据库 · ⑤ 交付与验证
-- 本文件覆盖 8 张表：change_sets, conflict_cases, scm_commands, scm_observations,
-- delivery_policies, validation_snapshots, database_branch_validations
-- （第 8 张 scm 侧的 repositories 轮询游标已并入 0007 的 repomesh_scan.repositories）

SET LOCAL search_path = public, pg_catalog;

CREATE TABLE IF NOT EXISTS public.change_sets (
  id uuid PRIMARY KEY,
  organization_id uuid NOT NULL,
  task_id uuid,
  repository_ids jsonb NOT NULL DEFAULT '[]'::jsonb,
  status text NOT NULL DEFAULT 'draft',
  pr_url text,
  branch text,
  payload jsonb NOT NULL DEFAULT '{}'::jsonb,
  version integer NOT NULL DEFAULT 1,
  idempotency_key text UNIQUE,
  fingerprint text NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_change_sets_status ON public.change_sets (status);

CREATE TABLE IF NOT EXISTS public.conflict_cases (
  id uuid PRIMARY KEY,
  change_set_id uuid NOT NULL,
  project_id uuid NOT NULL,
  repository_id text NOT NULL,
  candidate_head_sha text NOT NULL DEFAULT '',
  kind text NOT NULL DEFAULT 'content_conflict',
  expected_base_sha text,
  observed_base_sha text,
  detail jsonb NOT NULL DEFAULT '{}'::jsonb,
  status text NOT NULL DEFAULT 'open',
  repair_task_id uuid,
  version integer NOT NULL DEFAULT 1,
  detected_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  resolved_at timestamptz
);
CREATE INDEX IF NOT EXISTS idx_conflict_cases_change_set ON public.conflict_cases (change_set_id);
CREATE INDEX IF NOT EXISTS idx_conflict_cases_repository ON public.conflict_cases (repository_id);
CREATE INDEX IF NOT EXISTS idx_conflict_cases_status ON public.conflict_cases (status);
-- 同变更集+仓库最多一个未决案件（部分唯一索引）
CREATE UNIQUE INDEX IF NOT EXISTS uq_conflict_cases_open_per_repo
  ON public.conflict_cases (change_set_id, repository_id)
  WHERE status IN ('open', 'repairing');

CREATE TABLE IF NOT EXISTS public.scm_commands (
  id uuid PRIMARY KEY,
  change_set_id uuid NOT NULL,
  command_type text NOT NULL DEFAULT '',
  params jsonb NOT NULL DEFAULT '{}'::jsonb,
  result jsonb,
  retry_count integer NOT NULL DEFAULT 0,
  status text NOT NULL DEFAULT 'pending'
);
CREATE INDEX IF NOT EXISTS idx_scm_commands_change_set ON public.scm_commands (change_set_id);

CREATE TABLE IF NOT EXISTS public.scm_observations (
  id uuid PRIMARY KEY,
  provider text NOT NULL,
  source text NOT NULL,
  external_id text NOT NULL,
  event_type text NOT NULL DEFAULT '',
  payload jsonb NOT NULL DEFAULT '{}'::jsonb,
  status text NOT NULL DEFAULT 'pending',
  change_set_id uuid,
  repository_id text,
  attempts integer NOT NULL DEFAULT 0,
  version integer NOT NULL DEFAULT 1,
  last_error text,
  observed_at timestamptz NOT NULL DEFAULT now(),
  received_at timestamptz NOT NULL DEFAULT now(),
  claimed_at timestamptz,
  processed_at timestamptz,
  UNIQUE (provider, source, external_id)
);
CREATE INDEX IF NOT EXISTS idx_scm_observations_status ON public.scm_observations (status);

CREATE TABLE IF NOT EXISTS public.delivery_policies (
  id uuid PRIMARY KEY,
  organization_id uuid NOT NULL,
  repository_id text,
  auto_merge boolean NOT NULL DEFAULT false,
  base_branch text NOT NULL DEFAULT 'main',
  required_checks jsonb NOT NULL DEFAULT '[]'::jsonb,
  required_approvals integer NOT NULL DEFAULT 1,
  contract_gate boolean NOT NULL DEFAULT false,
  add_label boolean NOT NULL DEFAULT false,
  updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_delivery_policies_org ON public.delivery_policies (organization_id);
-- 组织默认一条 + 仓库覆盖一条：同一组织内仓库覆盖行唯一
CREATE UNIQUE INDEX IF NOT EXISTS uq_delivery_policies_repo
  ON public.delivery_policies (repository_id) WHERE repository_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS public.validation_snapshots (
  id uuid PRIMARY KEY,
  organization_id uuid NOT NULL,
  project_id uuid NOT NULL,
  object_id uuid,
  environment_hash text,
  status text NOT NULL DEFAULT 'passed',
  payload jsonb NOT NULL DEFAULT '{}'::jsonb,
  expires_at timestamptz
);

CREATE TABLE IF NOT EXISTS public.database_branch_validations (
  id uuid PRIMARY KEY,
  organization_id uuid NOT NULL,
  project_id uuid NOT NULL,
  repository_id text NOT NULL,
  task_id uuid,
  candidate_sha text NOT NULL DEFAULT '',
  source_database_ref text NOT NULL DEFAULT '',
  provider text NOT NULL DEFAULT '',
  provider_branch_ref text NOT NULL DEFAULT '',
  engine_version text NOT NULL DEFAULT '',
  idempotency_key text UNIQUE,
  request_hash text NOT NULL DEFAULT '',
  status text NOT NULL DEFAULT 'pending',
  failure_code text,
  cleanup_pending boolean NOT NULL DEFAULT false,
  results jsonb NOT NULL DEFAULT '{}'::jsonb
);
CREATE INDEX IF NOT EXISTS idx_dbv_project ON public.database_branch_validations (project_id);
CREATE INDEX IF NOT EXISTS idx_dbv_candidate ON public.database_branch_validations (candidate_sha);
CREATE INDEX IF NOT EXISTS idx_dbv_status ON public.database_branch_validations (status);
