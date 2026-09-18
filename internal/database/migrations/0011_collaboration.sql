-- 0011_collaboration.sql — Go 版重建数据库 · ④ 协作与上下文
-- 本文件覆盖 5 张表：messages, context_objects, context_bundles, context_deltas, context_access_events

SET LOCAL search_path = public, pg_catalog;

CREATE TABLE IF NOT EXISTS public.messages (
  id uuid PRIMARY KEY,
  organization_id uuid NOT NULL,
  project_id uuid NOT NULL,
  repository_id text,
  task_id uuid,
  sender_type text NOT NULL,
  sender_agent_id uuid,
  sender_user_id uuid,
  recipient_agent_id uuid,
  kind text NOT NULL DEFAULT '',
  subject text NOT NULL DEFAULT '',
  body text NOT NULL DEFAULT '',
  room_id text NOT NULL,
  status text NOT NULL DEFAULT 'sent',
  correlation_id uuid
);
CREATE INDEX IF NOT EXISTS idx_messages_project ON public.messages (project_id);
CREATE INDEX IF NOT EXISTS idx_messages_room ON public.messages (room_id);

CREATE TABLE IF NOT EXISTS public.context_objects (
  id uuid PRIMARY KEY,
  project_id uuid NOT NULL,
  repository_id text,
  kind text NOT NULL DEFAULT '',
  name text NOT NULL DEFAULT '',
  content text NOT NULL DEFAULT '',
  version_no integer NOT NULL DEFAULT 1,
  version_history jsonb NOT NULL DEFAULT '[]'::jsonb,
  status text NOT NULL DEFAULT 'draft',
  decided_by uuid,
  decision_reason text,
  superseded_by_version integer,
  source_ref jsonb,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_context_objects_project ON public.context_objects (project_id);

CREATE TABLE IF NOT EXISTS public.context_bundles (
  id uuid PRIMARY KEY,
  assignment_id uuid NOT NULL,
  name text NOT NULL DEFAULT '',
  items jsonb NOT NULL DEFAULT '[]'::jsonb,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_context_bundles_assignment ON public.context_bundles (assignment_id);

CREATE TABLE IF NOT EXISTS public.context_deltas (
  id uuid PRIMARY KEY,
  object_id uuid NOT NULL,
  base_version integer NOT NULL DEFAULT 1,
  changes jsonb NOT NULL DEFAULT '{}'::jsonb,
  synced_to jsonb NOT NULL DEFAULT '[]'::jsonb,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_context_deltas_object ON public.context_deltas (object_id);

CREATE TABLE IF NOT EXISTS public.context_access_events (
  id uuid PRIMARY KEY,
  object_id uuid NOT NULL,
  reader_agent_id uuid NOT NULL,
  read_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_context_access_object ON public.context_access_events (object_id);
