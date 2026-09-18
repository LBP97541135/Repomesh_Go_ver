-- 0014_runtime_ledger.sql — Go 版重建数据库 · ⑦ 运行账本
-- 本文件覆盖 12 张表：events, llm_usage, alert_rules, alert_events,
-- trace_sessions, trace_events, log_entries, recovery_cases,
-- recovery_decisions, recovery_operations, mcp_server_policies,
-- bootstrap_operations
-- 用户红线：告警、降级、日志、恢复、决策链全部保留。

SET LOCAL search_path = public, pg_catalog;

CREATE TABLE IF NOT EXISTS public.events (
  id uuid PRIMARY KEY,
  channel text NOT NULL,
  event_type text NOT NULL,
  occurred_at timestamptz NOT NULL DEFAULT now(),
  recorded_at timestamptz NOT NULL DEFAULT now(),
  actor_type text NOT NULL DEFAULT '',
  actor_id text NOT NULL DEFAULT '',
  organization_id uuid,
  task_id uuid,
  run_id uuid,
  correlation_id uuid NOT NULL,
  causation_id uuid,
  aggregate_type text NOT NULL DEFAULT '',
  aggregate_id uuid NOT NULL,
  aggregate_version integer NOT NULL DEFAULT 1,
  payload jsonb NOT NULL DEFAULT '{}'::jsonb,
  published_at timestamptz,
  attempts integer,
  last_error text
);
CREATE INDEX IF NOT EXISTS idx_events_channel ON public.events (channel);
CREATE INDEX IF NOT EXISTS idx_events_type ON public.events (event_type);
CREATE INDEX IF NOT EXISTS idx_events_correlation ON public.events (correlation_id);
CREATE INDEX IF NOT EXISTS idx_events_aggregate ON public.events (aggregate_id);
-- 发件箱通道：待发布扫描
CREATE INDEX IF NOT EXISTS idx_events_outbox_pending
  ON public.events (recorded_at) WHERE channel = 'outbox' AND published_at IS NULL;

CREATE TABLE IF NOT EXISTS public.llm_usage (
  id uuid PRIMARY KEY,
  organization_id uuid NOT NULL,
  agent_id uuid,
  task_id uuid,
  provider text NOT NULL DEFAULT '',
  model text NOT NULL DEFAULT '',
  operation text NOT NULL DEFAULT '',
  prompt_tokens integer NOT NULL DEFAULT 0,
  completion_tokens integer NOT NULL DEFAULT 0,
  duration_ms integer NOT NULL DEFAULT 0,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_llm_usage_org ON public.llm_usage (organization_id);
CREATE INDEX IF NOT EXISTS idx_llm_usage_created ON public.llm_usage (created_at);

CREATE TABLE IF NOT EXISTS public.alert_rules (
  id uuid PRIMARY KEY,
  organization_id uuid NOT NULL,
  name text NOT NULL DEFAULT '',
  metric text NOT NULL DEFAULT '',
  threshold jsonb NOT NULL DEFAULT '{}'::jsonb,
  enabled boolean NOT NULL DEFAULT true,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_alert_rules_org ON public.alert_rules (organization_id);

CREATE TABLE IF NOT EXISTS public.alert_events (
  id uuid PRIMARY KEY,
  organization_id uuid NOT NULL,
  rule_id uuid NOT NULL,
  metric_value jsonb NOT NULL DEFAULT '{}'::jsonb,
  message text NOT NULL DEFAULT '',
  status text NOT NULL DEFAULT 'fired',
  action text NOT NULL DEFAULT '',
  notification_status text NOT NULL DEFAULT 'pending',
  action_status text NOT NULL DEFAULT 'pending',
  error_code text,
  responded_at timestamptz NOT NULL DEFAULT now(),
  handled_by uuid,
  handled_at timestamptz,
  fired_at timestamptz NOT NULL DEFAULT now(),
  resolved_at timestamptz
);
CREATE INDEX IF NOT EXISTS idx_alert_events_rule ON public.alert_events (rule_id);
CREATE INDEX IF NOT EXISTS idx_alert_events_status ON public.alert_events (status);
CREATE INDEX IF NOT EXISTS idx_alert_events_fired ON public.alert_events (fired_at);
-- 每事件恰好一条响应（两阶段状态并入本表）
CREATE UNIQUE INDEX IF NOT EXISTS uq_alert_events_action
  ON public.alert_events (id, action);

CREATE TABLE IF NOT EXISTS public.trace_sessions (
  id uuid PRIMARY KEY,
  organization_id uuid NOT NULL,
  session_id text NOT NULL,
  agent_name text NOT NULL DEFAULT '',
  runtime text NOT NULL DEFAULT '',
  source_key text NOT NULL UNIQUE,
  object_mtime timestamptz NOT NULL DEFAULT now(),
  object_size integer NOT NULL DEFAULT 0,
  first_seen_at timestamptz NOT NULL DEFAULT now(),
  parsed_at timestamptz,
  parsing_error text,
  event_count integer NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_trace_sessions_agent ON public.trace_sessions (agent_name);
CREATE INDEX IF NOT EXISTS idx_trace_sessions_first_seen ON public.trace_sessions (first_seen_at);

CREATE TABLE IF NOT EXISTS public.trace_events (
  id uuid PRIMARY KEY,
  session_id uuid NOT NULL,
  seq integer NOT NULL,
  ts timestamptz NOT NULL DEFAULT now(),
  event_type text NOT NULL DEFAULT '',
  name text NOT NULL DEFAULT '',
  role text,
  summary text,
  token_count integer,
  latency_ms integer,
  status text NOT NULL DEFAULT '',
  payload jsonb,
  UNIQUE (session_id, seq)
);
CREATE INDEX IF NOT EXISTS idx_trace_events_session ON public.trace_events (session_id);

CREATE TABLE IF NOT EXISTS public.log_entries (
  id uuid PRIMARY KEY,
  organization_id uuid,
  level text NOT NULL DEFAULT 'info',
  source text NOT NULL DEFAULT '',
  message text NOT NULL DEFAULT '',
  context jsonb,
  logged_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_log_entries_level ON public.log_entries (level);
CREATE INDEX IF NOT EXISTS idx_log_entries_logged ON public.log_entries (logged_at);

CREATE TABLE IF NOT EXISTS public.bootstrap_operations (
  id uuid PRIMARY KEY,
  kind text NOT NULL DEFAULT '',
  state text NOT NULL DEFAULT 'pending',
  phase text NOT NULL DEFAULT '',
  attempt integer NOT NULL DEFAULT 0,
  lease_owner text,
  lease_expires_at timestamptz,
  error_code text,
  error_detail text,
  requested_at timestamptz NOT NULL DEFAULT now(),
  started_at timestamptz,
  finished_at timestamptz
);
CREATE INDEX IF NOT EXISTS idx_bootstrap_ops_state ON public.bootstrap_operations (state);

CREATE TABLE IF NOT EXISTS public.recovery_cases (
  id uuid PRIMARY KEY,
  object_type text NOT NULL,
  object_id uuid NOT NULL,
  reason text NOT NULL DEFAULT '',
  severity text NOT NULL DEFAULT 'normal',
  status text NOT NULL DEFAULT 'open',
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_recovery_cases_status ON public.recovery_cases (status);
CREATE INDEX IF NOT EXISTS idx_recovery_cases_object ON public.recovery_cases (object_type, object_id);

CREATE TABLE IF NOT EXISTS public.recovery_decisions (
  id uuid PRIMARY KEY,
  case_id uuid NOT NULL,
  decision text NOT NULL DEFAULT '',
  decided_by text NOT NULL DEFAULT '',
  decided_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_recovery_decisions_case ON public.recovery_decisions (case_id);

CREATE TABLE IF NOT EXISTS public.recovery_operations (
  id uuid PRIMARY KEY,
  case_id uuid NOT NULL,
  operation text NOT NULL DEFAULT '',
  params jsonb NOT NULL DEFAULT '{}'::jsonb,
  result jsonb NOT NULL DEFAULT '{}'::jsonb,
  executed_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_recovery_operations_case ON public.recovery_operations (case_id);

CREATE TABLE IF NOT EXISTS public.mcp_server_policies (
  server_name text PRIMARY KEY,
  timeout_seconds integer NOT NULL DEFAULT 30,
  max_retries integer NOT NULL DEFAULT 0,
  retryable_only_reads boolean NOT NULL DEFAULT true,
  degraded_block_writes boolean NOT NULL DEFAULT true,
  required_task_features jsonb NOT NULL DEFAULT '[]'::jsonb
);
