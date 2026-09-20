-- 0059: E 跨仓职责的三处 id 列类型改对 —— 它们被定义成 uuid，而平台存的是文本。
--
-- 背景（2026-09-20 实测，不是推理）：在控制台点「确认仓库 Owner」必然 500：
--     ERROR: invalid input syntax for type uuid: "repo_…"  (SQLSTATE 22P02)
-- 直接打端点同样 500，所以 `case_events` / `repo_owner_confirmations` /
-- `authorization_requests` 三张表至今 **0 行** —— 不是"没人用"，是**这条路走不通**。
--
-- 根因是 0052 建表时按"id 都该是 uuid"的直觉写了 uuid，而这个库的 id 有两套口径：
--   · `public.plans.id` / `public.plans.project_id`   → uuid   （0052 写对了）
--   · `repomesh_issues.issues.id`                     → text，形如 `iss_…`
--   · 项目侧仓库 id                                    → text，形如 `repo_…`
-- 于是这三列在写入时必然撞 22P02：
--   · `case_events.issue_id`                     —— RecordEvent 里 `NULLIF($2,'')::uuid`
--   · `repo_owner_confirmations.repository_id`   —— ConfirmOwner
--   · `authorization_requests.repository_id`     —— RequestAuth
--
-- 改法：三列改 text，与它们**实际要装的 id** 对齐。`plan_id` / `project_id` 不动
-- （它们对的确实是 uuid）。`USING …::text` 显式转换，避免依赖隐式赋值转换的细节。
--
-- 幂等：用 information_schema 判当前类型，只有还是 uuid 时才改（可重跑）。
-- 数据：三张表线上都是 0 行，改类型不涉及存量数据搬迁；即便有，uuid→text 也无损。

SET LOCAL search_path = public, pg_catalog;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.columns
               WHERE table_schema='public' AND table_name='case_events'
                 AND column_name='issue_id' AND data_type='uuid') THEN
        ALTER TABLE public.case_events
            ALTER COLUMN issue_id TYPE text USING issue_id::text;
    END IF;

    IF EXISTS (SELECT 1 FROM information_schema.columns
               WHERE table_schema='public' AND table_name='repo_owner_confirmations'
                 AND column_name='repository_id' AND data_type='uuid') THEN
        ALTER TABLE public.repo_owner_confirmations
            ALTER COLUMN repository_id TYPE text USING repository_id::text;
    END IF;

    IF EXISTS (SELECT 1 FROM information_schema.columns
               WHERE table_schema='public' AND table_name='authorization_requests'
                 AND column_name='repository_id' AND data_type='uuid') THEN
        ALTER TABLE public.authorization_requests
            ALTER COLUMN repository_id TYPE text USING repository_id::text;
    END IF;
END $$;

COMMENT ON COLUMN public.case_events.issue_id IS
  '0059：改 text —— issue id 是 `iss_…`（repomesh_issues.issues.id 是 text），原 uuid 定义会让每次记录都 22P02。';
COMMENT ON COLUMN public.repo_owner_confirmations.repository_id IS
  '0059：改 text —— 项目侧仓库 id 形如 `repo_…`（text），原 uuid 定义会让 Owner 确认必 500。';
COMMENT ON COLUMN public.authorization_requests.repository_id IS
  '0059：改 text —— 同 repo_owner_confirmations，理由一致。';
