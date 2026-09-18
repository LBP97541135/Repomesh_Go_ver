-- Observability v1 API surface (ObserveHome/Usage/Logs/Alerts/Trace pages):
-- columns the Py-version-shaped read models need that 0014 did not carry.
ALTER TABLE public.llm_usage ADD COLUMN IF NOT EXISTS issue_id uuid;
ALTER TABLE public.llm_usage ADD COLUMN IF NOT EXISTS status text NOT NULL DEFAULT 'ok';
ALTER TABLE public.llm_usage ADD COLUMN IF NOT EXISTS finish_reason text;
ALTER TABLE public.llm_usage ADD COLUMN IF NOT EXISTS estimated_cost_usd numeric(12,6) NOT NULL DEFAULT 0;
ALTER TABLE public.llm_usage ADD COLUMN IF NOT EXISTS step integer;
CREATE INDEX IF NOT EXISTS idx_llm_usage_issue ON public.llm_usage (issue_id) WHERE issue_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_llm_usage_created_desc ON public.llm_usage (created_at DESC);

ALTER TABLE public.log_entries ADD COLUMN IF NOT EXISTS issue_id uuid;
CREATE INDEX IF NOT EXISTS idx_log_entries_issue ON public.log_entries (issue_id) WHERE issue_id IS NOT NULL;

ALTER TABLE public.alert_rules ADD COLUMN IF NOT EXISTS operator text NOT NULL DEFAULT 'gt'
    CHECK (operator IN ('lt', 'gt'));
ALTER TABLE public.alert_rules ADD COLUMN IF NOT EXISTS window_minutes integer NOT NULL DEFAULT 60;
ALTER TABLE public.alert_rules ADD COLUMN IF NOT EXISTS threshold_value numeric NOT NULL DEFAULT 0;
ALTER TABLE public.alert_rules ADD COLUMN IF NOT EXISTS updated_at timestamptz NOT NULL DEFAULT now();

ALTER TABLE public.alert_events ADD COLUMN IF NOT EXISTS value numeric NOT NULL DEFAULT 0;
ALTER TABLE public.alert_events ADD COLUMN IF NOT EXISTS window_minutes integer NOT NULL DEFAULT 60;
ALTER TABLE public.alert_events ADD COLUMN IF NOT EXISTS status_v1 text NOT NULL DEFAULT 'firing';

-- trace sessions keyed by external session string; issue attribution via usage windows
ALTER TABLE public.trace_sessions ADD COLUMN IF NOT EXISTS organization_key text NOT NULL DEFAULT '';
