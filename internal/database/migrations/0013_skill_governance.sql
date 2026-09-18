-- 0013_skill_governance.sql — Go 版重建数据库 · ⑥ Skill 体系（7 张全新表）
-- 定稿方案：出题 → AB 评估 → 盲选 → 逐级审核 → 绑定 → 沉淀
-- 逐级审核定稿：Worker 的 Manager 审、Manager 的 Leader 审、Leader 的人工审；异步不阻塞。
-- 表结构 = 原插件 capability_management schema 的 7 张 skill 表按定稿文档逐列落地，
-- 列名与 internal/skills 包的 SQL 完全对齐。

SET LOCAL search_path = public, pg_catalog;

CREATE TABLE IF NOT EXISTS public.skills (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  name text NOT NULL UNIQUE,
  scenario text NOT NULL,
  target_agent_role text NOT NULL,
  created_by text NOT NULL DEFAULT '',
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS public.skill_versions (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  skill_id uuid NOT NULL,
  version text NOT NULL,
  status text NOT NULL DEFAULT 'draft',
  content text NOT NULL,
  content_hash text NOT NULL DEFAULT '',
  created_by text NOT NULL DEFAULT '',
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (skill_id, version)
);
CREATE INDEX IF NOT EXISTS idx_skill_versions_status ON public.skill_versions (status);

CREATE TABLE IF NOT EXISTS public.skill_test_questions (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  skill_id uuid NOT NULL,
  kind text NOT NULL,
  question text NOT NULL,
  expected jsonb NOT NULL DEFAULT '{}'::jsonb,
  provided_by text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_skill_questions_skill ON public.skill_test_questions (skill_id);

CREATE TABLE IF NOT EXISTS public.skill_evaluation_runs (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  version_id uuid NOT NULL,
  question_id uuid NOT NULL,
  arm text NOT NULL,
  blinded_label text NOT NULL,
  answer jsonb NOT NULL DEFAULT '{}'::jsonb,
  judged_by text,
  result text NOT NULL,
  run_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_eval_runs_version ON public.skill_evaluation_runs (version_id);

CREATE TABLE IF NOT EXISTS public.skill_approvals (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  version_id uuid NOT NULL UNIQUE,
  subject_role text NOT NULL,
  reviewer_kind text NOT NULL,
  reviewer_user_id text,
  review_status text NOT NULL DEFAULT 'pending',
  reviewed_at timestamptz,
  review_note text,
  recused boolean NOT NULL DEFAULT false,
  conclusion text,
  decided_at timestamptz
);

CREATE TABLE IF NOT EXISTS public.agent_skill_bindings (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  agent_id uuid NOT NULL,
  version_id uuid NOT NULL,
  source text NOT NULL DEFAULT 'approval_release',
  bound_at timestamptz NOT NULL DEFAULT now(),
  active boolean NOT NULL DEFAULT true
);
CREATE INDEX IF NOT EXISTS idx_bindings_agent_active
  ON public.agent_skill_bindings (agent_id) WHERE active;

CREATE TABLE IF NOT EXISTS public.skill_update_suggestions (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  task_id uuid,
  skill_id uuid NOT NULL,
  suggestion text NOT NULL,
  status text NOT NULL DEFAULT 'pending',
  decided_by text,
  decided_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_skill_suggestions_skill ON public.skill_update_suggestions (skill_id);

-- 功能开关行（仿 0008 decision_chain）：internal/skills 全端点据此 503
INSERT INTO public.feature_settings (feature, enabled) VALUES ('skill_governance', true)
ON CONFLICT (feature) DO NOTHING;
