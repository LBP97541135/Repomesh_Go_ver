-- 0053: 组织退出主业务 —— 编制（public.agents）的幂等键从「组织」改成「项目」。
--
-- 为什么改（2026-09-20 用户裁定）：「组织」只回答「这是哪个账号的数据」，
-- 不参与业务怎么分组。业务链是 账号 → 项目 → issue → 仓库。
--
-- 但编制的幂等键此前是 组织:角色:仓库:名字，而组织与账号当前 1:1（0037），
-- 于是**同一账号下的两个项目挂同一个仓库**时，两套编制会撞成同一个人（串）。
-- ADR-0001 D02 定的是「一个多仓库项目由一个 Manager 负责」——编制根本来就在项目上。
--
-- 做法（幂等，可重跑；不改任何既有迁移）：
--   1) agents 加 project_id。organization_id 保留，降级为**只写冗余租户戳**。
--   2) 按 agent_teams 反查既有编制的项目归属（唯一可确定的来源）并回填。
--   3) 回填到项目的行，singleton_key 去掉组织前缀，统一成 角色:仓库:名字；
--      项目总领导按项目派生名字（leader-<项目 id 后 12 位>），与 assembly.shortName
--      的口径一致，使重复装配保持幂等。
--   4) 唯一性拆成两条：有项目的按 (project_id, singleton_key) 幂等，
--      没有项目的（治理 leader 等组织级角色）仍按 (organization_id, singleton_key)。
--
-- 回填不到的遗留行（例如 role=organization_leader、singleton_key='governance-leader'
-- 这种不含冒号的键）**原样保留**：它们不是自动编制的产物，项目归属无法确定，
-- 强行归属就是编造。它们继续按组织幂等，业务不受影响。

SET LOCAL search_path = public, pg_catalog;

-- 1) 项目维度
ALTER TABLE public.agents ADD COLUMN IF NOT EXISTS project_id text;

-- 2) 从团队行反查项目归属（团队行是三处引用：leader / manager / worker 列表）
WITH team_agents AS (
    SELECT t.project_id::text AS project_id, t.leader_agent_id AS agent_id
      FROM public.agent_teams t
     WHERE t.leader_agent_id IS NOT NULL
    UNION
    SELECT t.project_id::text, t.manager_agent_id
      FROM public.agent_teams t
     WHERE t.manager_agent_id IS NOT NULL
    UNION
    SELECT t.project_id::text, (w.value)::uuid
      FROM public.agent_teams t
      CROSS JOIN LATERAL jsonb_array_elements_text(t.worker_agent_ids) AS w(value)
     WHERE w.value ~ '^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$'
)
UPDATE public.agents a
   SET project_id = resolved.project_id
  FROM (SELECT agent_id, min(project_id) AS project_id FROM team_agents GROUP BY agent_id) AS resolved
 WHERE a.id = resolved.agent_id
   AND a.project_id IS NULL;

-- 3) 幂等键：去掉组织前缀；项目总领导按项目派生名字
DROP INDEX IF EXISTS public.agents_org_singleton_key_key;

UPDATE public.agents
   SET singleton_key = regexp_replace(singleton_key, '^[^:]+:', '')
 WHERE project_id IS NOT NULL
   AND singleton_key ~ '^[^:]+:';

UPDATE public.agents
   SET singleton_key = 'leader::leader-' || right(project_id, 12)
 WHERE project_id IS NOT NULL
   AND role = 'leader'
   AND COALESCE(repository_id, '') = '';

-- 4) 唯一性：项目级与组织级各一条（同一条键不会同时落在两个索引的谓词里）
CREATE UNIQUE INDEX IF NOT EXISTS agents_project_singleton_key_key
    ON public.agents(project_id, singleton_key)
 WHERE project_id IS NOT NULL AND singleton_key IS NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS agents_org_singleton_key_key
    ON public.agents(organization_id, singleton_key)
 WHERE project_id IS NULL AND singleton_key IS NOT NULL;

COMMENT ON COLUMN public.agents.project_id IS
    '编制归属的项目（业务键，0053）。为空 = 组织级角色（治理 leader、规划 agent），仍按 organization_id 幂等。';
COMMENT ON COLUMN public.agents.organization_id IS
    '冗余租户戳：只回答「这是哪个账号的数据」，不参与业务分组（0053）。';
