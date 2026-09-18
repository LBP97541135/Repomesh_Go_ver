-- 0008_decision_chain.sql — 历史决策（decision chain）落地
-- 改动清单：docs/decision_chain_nodes-decision_embeddings-feature_settings改动清单-2026-09-15.md
-- 写法：全新库走 CREATE TABLE IF NOT EXISTS 全形状；已按 44 表方案建好
-- decision_chain_nodes / decision_embeddings 的库走 ADD COLUMN IF NOT EXISTS 补列。
-- pgvector 是向量主能力的必装扩展（D11）：缺失时本迁移显式失败，装好重跑即可。
--
-- 本仓库连接默认 search_path = pg_catalog（internal/database.Open 的约定，
-- 强制 schema 全限定名）。下面 SET LOCAL 只在本迁移事务内生效，让扩展对象
-- 落到 public、HNSW 算子类可解析；业务 SQL 一律 public. 全限定。
-- 注意：SET LOCAL 持续到 Migrate 事务结束——后续迁移（0009+）若依赖
-- pg_catalog 优先解析，必须自行 SET LOCAL 或全限定。

SET LOCAL search_path = public, pg_catalog;

CREATE EXTENSION IF NOT EXISTS vector;

CREATE TABLE IF NOT EXISTS public.decision_chain_nodes (
  id uuid PRIMARY KEY,
  project_id uuid,
  parent_node_id uuid,
  actor_type text NOT NULL DEFAULT 'human',
  actor_id text NOT NULL DEFAULT '',
  action text NOT NULL DEFAULT '',
  rationale text NOT NULL DEFAULT '',
  context_ref jsonb NOT NULL DEFAULT '{}',
  created_at timestamptz NOT NULL DEFAULT now(),
  event_id text,
  requirement_text text NOT NULL DEFAULT '',
  requirement_key text NOT NULL DEFAULT '',
  step text NOT NULL DEFAULT 'confirmation',
  version integer NOT NULL DEFAULT 1,
  status text NOT NULL DEFAULT 'proposed',
  affected_repositories jsonb NOT NULL DEFAULT '[]',
  source text NOT NULL DEFAULT 'event'
);

ALTER TABLE public.decision_chain_nodes ADD COLUMN IF NOT EXISTS event_id text;
ALTER TABLE public.decision_chain_nodes ADD COLUMN IF NOT EXISTS requirement_text text NOT NULL DEFAULT '';
ALTER TABLE public.decision_chain_nodes ADD COLUMN IF NOT EXISTS requirement_key text NOT NULL DEFAULT '';
ALTER TABLE public.decision_chain_nodes ADD COLUMN IF NOT EXISTS step text NOT NULL DEFAULT 'confirmation';
ALTER TABLE public.decision_chain_nodes ADD COLUMN IF NOT EXISTS version integer NOT NULL DEFAULT 1;
ALTER TABLE public.decision_chain_nodes ADD COLUMN IF NOT EXISTS status text NOT NULL DEFAULT 'proposed';
ALTER TABLE public.decision_chain_nodes ADD COLUMN IF NOT EXISTS affected_repositories jsonb NOT NULL DEFAULT '[]';
ALTER TABLE public.decision_chain_nodes ADD COLUMN IF NOT EXISTS source text NOT NULL DEFAULT 'event';

CREATE UNIQUE INDEX IF NOT EXISTS uq_decision_nodes_event_id
  ON public.decision_chain_nodes (event_id) WHERE event_id IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS uq_decision_nodes_version
  ON public.decision_chain_nodes (requirement_key, step, version) WHERE requirement_key <> '';
CREATE INDEX IF NOT EXISTS idx_decision_nodes_repos
  ON public.decision_chain_nodes USING gin (affected_repositories);
CREATE INDEX IF NOT EXISTS idx_decision_nodes_project
  ON public.decision_chain_nodes (project_id);

CREATE TABLE IF NOT EXISTS public.decision_embeddings (
  id uuid PRIMARY KEY,
  node_id uuid NOT NULL UNIQUE,
  embedding jsonb NOT NULL,
  model text NOT NULL DEFAULT '',
  embedding_vec vector(1024),
  embedded_at timestamptz
);

ALTER TABLE public.decision_embeddings ADD COLUMN IF NOT EXISTS embedding_vec vector(1024);
ALTER TABLE public.decision_embeddings ADD COLUMN IF NOT EXISTS embedded_at timestamptz;

CREATE INDEX IF NOT EXISTS idx_decision_embeddings_hnsw
  ON public.decision_embeddings USING hnsw (embedding_vec vector_cosine_ops);

CREATE TABLE IF NOT EXISTS public.feature_settings (
  feature text PRIMARY KEY,
  enabled boolean NOT NULL DEFAULT true,
  updated_by text NOT NULL DEFAULT '',
  updated_at timestamptz NOT NULL DEFAULT now()
);

INSERT INTO public.feature_settings (feature, enabled) VALUES ('decision_chain', true)
ON CONFLICT (feature) DO NOTHING;
