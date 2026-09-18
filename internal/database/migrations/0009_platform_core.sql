-- 0009_platform_core.sql — Go 版重建数据库 · ① 账号与身份 + ② 项目与编制
-- 方案来源：RepoMesh_Go版数据库重构方案.html（.claude/gen_go_blueprint.py 生成）
-- 本文件覆盖 8 张表：organizations, users, agents, credentials, projects, plans, agent_teams, review_requests
-- 写法说明（仿 0008）：
--   * 本仓库连接默认 search_path = pg_catalog（internal/database.Open 的约定），
--     SET LOCAL 只在本迁移事务内生效，让 gen_random_uuid() 可解析。
--   * 全部 public. 全限定；CREATE TABLE IF NOT EXISTS 幂等。
--   * repositories 表在 0007 已建（repomesh_scan.repositories），多仓库归属列
--     organization_id 由此迁移补齐，不另建新表。
--   * 外键从简（对齐 0008 风格：逻辑关联，不加 DB 级硬外键，避免跨迁移循环依赖）。

SET LOCAL search_path = public, pg_catalog;

-- ============ ① 账号与身份 ============

CREATE TABLE IF NOT EXISTS public.organizations (
  id uuid PRIMARY KEY,
  name text NOT NULL DEFAULT '',
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS public.users (
  id uuid PRIMARY KEY,
  organization_id uuid NOT NULL,
  username text NOT NULL UNIQUE,
  display_name text NOT NULL DEFAULT '',
  password_hash text NOT NULL DEFAULT '',
  org_role text NOT NULL DEFAULT 'member',
  active boolean NOT NULL DEFAULT true,
  session_token_hash text,
  session_expires_at timestamptz
);
CREATE INDEX IF NOT EXISTS idx_users_org ON public.users (organization_id);

CREATE TABLE IF NOT EXISTS public.agents (
  id uuid PRIMARY KEY,
  organization_id uuid NOT NULL,
  role text NOT NULL,
  parent_agent_id uuid,
  repository_id text,
  responsibility_paths jsonb NOT NULL DEFAULT '[]'::jsonb,
  resource_ref jsonb NOT NULL DEFAULT '{}'::jsonb,
  singleton_key text UNIQUE,
  status text NOT NULL DEFAULT 'active',
  idempotency_key text UNIQUE
);
CREATE INDEX IF NOT EXISTS idx_agents_role ON public.agents (role);
CREATE INDEX IF NOT EXISTS idx_agents_org ON public.agents (organization_id);

CREATE TABLE IF NOT EXISTS public.credentials (
  id uuid PRIMARY KEY,
  kind text NOT NULL,
  key text NOT NULL UNIQUE,
  value_encrypted text NOT NULL DEFAULT '',
  app_id text NOT NULL DEFAULT '',
  installation_id text NOT NULL DEFAULT '',
  owner_login text NOT NULL DEFAULT '',
  updated_by text NOT NULL DEFAULT '',
  updated_at timestamptz NOT NULL DEFAULT now()
);

-- ============ ② 项目与编制 ============

CREATE TABLE IF NOT EXISTS public.projects (
  id uuid PRIMARY KEY,
  organization_id uuid NOT NULL,
  repository_id text NOT NULL UNIQUE,
  name text NOT NULL DEFAULT '',
  status text NOT NULL DEFAULT 'active',
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_projects_org ON public.projects (organization_id);

-- 0007 的 repomesh_scan.repositories 补多租户组织列（Go 版语义增强）
ALTER TABLE repomesh_scan.repositories ADD COLUMN IF NOT EXISTS organization_id uuid;
CREATE INDEX IF NOT EXISTS idx_scan_repositories_org ON repomesh_scan.repositories (organization_id);

CREATE TABLE IF NOT EXISTS public.plans (
  id uuid PRIMARY KEY,
  project_id uuid NOT NULL,
  plan_version text NOT NULL DEFAULT 'v1',
  requirement_text text NOT NULL DEFAULT '',
  specs jsonb NOT NULL DEFAULT '[]'::jsonb,
  task_dag jsonb NOT NULL DEFAULT '{}'::jsonb,
  execution_batches jsonb NOT NULL DEFAULT '[]'::jsonb,
  revisions jsonb NOT NULL DEFAULT '[]'::jsonb,
  integration_method text,
  execution_plan_id uuid,
  created_by_agent_id uuid
);
CREATE INDEX IF NOT EXISTS idx_plans_project ON public.plans (project_id);

CREATE TABLE IF NOT EXISTS public.agent_teams (
  id uuid PRIMARY KEY,
  project_id uuid NOT NULL,
  repository_id text,
  leader_agent_id uuid NOT NULL,
  manager_agent_id uuid NOT NULL,
  worker_agent_ids jsonb NOT NULL DEFAULT '[]'::jsonb,
  team_name text,
  room_id text,
  execution_mode text NOT NULL DEFAULT '',
  required_checkpoints jsonb NOT NULL DEFAULT '[]'::jsonb,
  human_grants jsonb NOT NULL DEFAULT '[]'::jsonb,
  runtime_status text NOT NULL DEFAULT 'idle',
  idempotency_key text UNIQUE
);
CREATE INDEX IF NOT EXISTS idx_agent_teams_project ON public.agent_teams (project_id);

CREATE TABLE IF NOT EXISTS public.review_requests (
  id uuid PRIMARY KEY,
  project_id uuid NOT NULL,
  object_type text NOT NULL,
  object_id uuid NOT NULL,
  request_content jsonb NOT NULL DEFAULT '{}'::jsonb,
  status text NOT NULL DEFAULT 'pending',
  requested_by_agent_id uuid,
  decided_by uuid,
  decision_note text,
  decided_at timestamptz
);
CREATE INDEX IF NOT EXISTS idx_review_requests_project ON public.review_requests (project_id);
CREATE INDEX IF NOT EXISTS idx_review_requests_status ON public.review_requests (status);
