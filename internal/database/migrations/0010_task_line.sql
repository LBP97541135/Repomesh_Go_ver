-- 0010_task_line.sql — Go 版重建数据库 · ③ 任务主线
-- 本文件覆盖 5 张表：tasks, plan_steps, task_assignments, executions, handoffs
-- executions 的部分唯一索引保证"同任务同时最多一个活跃占位"。

SET LOCAL search_path = public, pg_catalog;

CREATE TABLE IF NOT EXISTS public.tasks (
  id uuid PRIMARY KEY,
  organization_id uuid NOT NULL,
  project_id uuid NOT NULL,
  repository_id text,
  parent_task_id uuid,
  title text NOT NULL DEFAULT '',
  instruction text NOT NULL DEFAULT '',
  acceptance text NOT NULL DEFAULT '',
  source_ref jsonb,
  status text NOT NULL DEFAULT 'pending',
  assigned_by_agent_id uuid,
  assignee_agent_id uuid,
  result_summary text,
  version integer NOT NULL DEFAULT 1,
  idempotency_key text UNIQUE
);
CREATE INDEX IF NOT EXISTS idx_tasks_project ON public.tasks (project_id);
CREATE INDEX IF NOT EXISTS idx_tasks_status ON public.tasks (status);

CREATE TABLE IF NOT EXISTS public.plan_steps (
  id uuid PRIMARY KEY,
  plan_id uuid NOT NULL,
  step_no integer NOT NULL,
  content text NOT NULL DEFAULT '',
  assignee_role text NOT NULL DEFAULT 'worker',
  depends_on jsonb NOT NULL DEFAULT '[]'::jsonb,
  status text NOT NULL DEFAULT 'pending',
  output jsonb
);
CREATE INDEX IF NOT EXISTS idx_plan_steps_plan ON public.plan_steps (plan_id);
CREATE INDEX IF NOT EXISTS idx_plan_steps_status ON public.plan_steps (status);

CREATE TABLE IF NOT EXISTS public.task_assignments (
  id uuid PRIMARY KEY,
  task_id uuid NOT NULL,
  step_id uuid,
  manager_agent_id uuid NOT NULL,
  worker_agent_id uuid NOT NULL,
  phase text NOT NULL DEFAULT '',
  safety_envelope jsonb NOT NULL DEFAULT '{}'::jsonb,
  generation integer NOT NULL DEFAULT 1,
  attempt_state text NOT NULL DEFAULT 'dispatched',
  attempt_reason text,
  previous_attempt_id uuid,
  execution_id uuid,
  finished_at timestamptz
);
CREATE INDEX IF NOT EXISTS idx_task_assignments_task ON public.task_assignments (task_id);

CREATE TABLE IF NOT EXISTS public.executions (
  id uuid PRIMARY KEY,
  run_id uuid NOT NULL UNIQUE,
  organization_id uuid NOT NULL,
  project_id uuid NOT NULL,
  repository_id text,
  task_id uuid NOT NULL,
  worker_agent_id uuid,
  channel text NOT NULL DEFAULT 'internal',
  status text NOT NULL DEFAULT 'reserved',
  attempt integer NOT NULL DEFAULT 1,
  version integer NOT NULL DEFAULT 1,
  lease_owner text,
  lease_until timestamptz,
  task_payload jsonb,
  error_detail text,
  idempotency_key text UNIQUE,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  completed_at timestamptz
);
CREATE INDEX IF NOT EXISTS idx_executions_task ON public.executions (task_id);
CREATE INDEX IF NOT EXISTS idx_executions_status ON public.executions (status);
-- 同任务同时最多一个活跃占位（部分唯一索引）
CREATE UNIQUE INDEX IF NOT EXISTS uq_executions_active_per_task
  ON public.executions (task_id)
  WHERE status IN ('reserved', 'leased', 'running');

CREATE TABLE IF NOT EXISTS public.handoffs (
  id uuid PRIMARY KEY,
  task_id uuid NOT NULL,
  branch_validation_key text NOT NULL UNIQUE,
  payload jsonb NOT NULL DEFAULT '{}'::jsonb,
  evidence jsonb NOT NULL DEFAULT '{}'::jsonb,
  status text NOT NULL DEFAULT 'pending',
  created_at timestamptz NOT NULL DEFAULT now()
);
