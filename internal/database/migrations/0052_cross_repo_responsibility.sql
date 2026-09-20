-- 0052: E 跨仓职责 / 授权 / 冲突。
--
-- 六张新表：案例时间线（三角色交接可见）、Owner 确认、授权申请与撤销、
-- 责任转移、冲突裁决、平台↔应用状态关联。

SET LOCAL search_path = public, pg_catalog;

-- 1. 案例时间线：每个 plan 的不可变事件流。
CREATE TABLE IF NOT EXISTS public.case_events (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    plan_id uuid NOT NULL,
    issue_id uuid,
    project_id uuid NOT NULL,
    actor_role text NOT NULL CHECK (actor_role IN ('manager','leader','worker','human','system')),
    actor_id text NOT NULL DEFAULT '',
    event_kind text NOT NULL CHECK (event_kind IN (
        'dependency_analysis','task_assigned','task_blocked','task_completed',
        'opinion_conflict','conflict_resolved','escalation',
        'owner_confirmed','auth_requested','auth_granted','auth_revoked',
        'responsibility_transferred','status_synced')),
    detail jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_case_events_plan ON public.case_events (plan_id, created_at);

-- 2. 仓库 Owner 确认。
CREATE TABLE IF NOT EXISTS public.repo_owner_confirmations (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    plan_id uuid NOT NULL,
    repository_id uuid NOT NULL,
    owner_github_id text NOT NULL,
    confirmed_by text NOT NULL DEFAULT '',
    status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','confirmed','rejected')),
    confirmed_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX IF NOT EXISTS uq_repo_owner_plan_repo
    ON public.repo_owner_confirmations (plan_id, repository_id);

-- 3. 授权申请与撤销。
CREATE TABLE IF NOT EXISTS public.authorization_requests (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    plan_id uuid NOT NULL,
    repository_id uuid NOT NULL,
    requester_id text NOT NULL,
    requester_role text NOT NULL CHECK (requester_role IN ('manager','leader','worker')),
    scope text NOT NULL DEFAULT 'write' CHECK (scope IN ('read','write')),
    status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','granted','revoked','expired')),
    granted_by text,
    granted_at timestamptz,
    revoked_by text,
    revoked_at timestamptz,
    reason text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_auth_req_plan ON public.authorization_requests (plan_id, status);

-- 4. 责任转移。
CREATE TABLE IF NOT EXISTS public.responsibility_transfers (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    plan_id uuid NOT NULL,
    from_role text NOT NULL,
    from_id text NOT NULL,
    to_role text NOT NULL,
    to_id text NOT NULL,
    reason text NOT NULL DEFAULT '',
    transferred_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_resp_transfer_plan ON public.responsibility_transfers (plan_id);

-- 5. 冲突裁决。
CREATE TABLE IF NOT EXISTS public.conflict_resolutions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    plan_id uuid NOT NULL,
    conflict_type text NOT NULL CHECK (conflict_type IN ('graph_conflict','opinion_conflict','scope_conflict')),
    discovered_by text NOT NULL,
    discovered_by_role text NOT NULL,
    resolved_by text,
    resolved_by_role text,
    resolution text NOT NULL DEFAULT '',
    status text NOT NULL DEFAULT 'open' CHECK (status IN ('open','resolved','escalated_to_human')),
    resolved_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_conflict_res_plan ON public.conflict_resolutions (plan_id, status);

-- 6. 平台任务状态列。
ALTER TABLE public.tasks ADD COLUMN IF NOT EXISTS platform_status text NOT NULL DEFAULT 'dag_ready';
ALTER TABLE public.tasks DROP CONSTRAINT IF EXISTS tasks_platform_status_check;
ALTER TABLE public.tasks ADD CONSTRAINT tasks_platform_status_check
    CHECK (platform_status IN ('dag_ready','dispatched','running','blocked','done'));
