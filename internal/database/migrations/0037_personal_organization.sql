-- 0037: 每账号一个私有组织（完整账号隔离，2026-09-19 用户裁定
-- 「能不能做出完整的账号隔离，每一个账号的全部信息都是隔离的」）。
--
-- 背景：0036 把**所有**账号都归到「默认组织」，等于所有人共享一个租户——
-- 项目 / issue / 智能体 / 任务互相可见。租户原语（public.organizations）本身没问题，
-- 缺的是**默认一人一个组织**：注册即分家。
--
-- 做法（幂等，可重跑）：
--   1) 每个启用账号建一个私有组织（按名字 `<display_name> 的个人空间` 去重），
--      账号归属指向它。将来要多人协作 = 把多个账号指向同一个 organization，
--      所以租户原语保留，不新增概念。
--   2) 旧共享组织里剩下的数据（项目/任务/智能体/扫描仓库）整体归给
--      「拥有该项目那个账号」的私有组织；singleton_key 里的组织前缀同步改写。
--   3) agents.singleton_key 的**全局**唯一约束改成 (organization_id, singleton_key)
--      ——否则第二个组织的 leader/manager/worker 会撞唯一键，编制根本建不出来。
--
-- skills 的账号维度不在本迁移里（见后续），因为它的名字唯一约束被代码里的
-- `ON CONFLICT (name)` 依赖，必须与代码改动同时上线。

-- 1) 私有组织 + 账号归属
DO $$
DECLARE
    shared uuid;
    rec RECORD;
    org uuid;
BEGIN
    SELECT id INTO shared FROM public.organizations ORDER BY created_at, id LIMIT 1;
    IF shared IS NULL THEN
        RETURN;
    END IF;

    FOR rec IN
        SELECT a.id, a.display_name
          FROM repomesh_access.accounts a
         WHERE a.disabled = false AND a.organization_id = shared
    LOOP
        SELECT o.id INTO org
          FROM public.organizations o
         WHERE o.name = rec.display_name || ' 的个人空间';
        IF org IS NULL THEN
            INSERT INTO public.organizations(id, name)
            VALUES (gen_random_uuid(), rec.display_name || ' 的个人空间')
            RETURNING id INTO org;
        END IF;
        UPDATE repomesh_access.accounts SET organization_id = org WHERE id = rec.id;
    END LOOP;
END $$;

-- 2) 旧共享组织里的数据跟着账号走
DO $$
DECLARE
    shared uuid;
    target uuid;
BEGIN
    SELECT id INTO shared FROM public.organizations ORDER BY created_at, id LIMIT 1;
    IF shared IS NULL THEN
        RETURN;
    END IF;

    -- 旧组织里第一个项目的 owner 的私有组织就是这批数据的新家
    SELECT a.organization_id INTO target
      FROM repomesh_projects.projects p
      JOIN repomesh_access.accounts a ON a.id = p.owner
     WHERE p.organization_id = shared
     ORDER BY p.created_at
     LIMIT 1;
    IF target IS NULL OR target = shared THEN
        RETURN;
    END IF;

    UPDATE repomesh_projects.projects SET organization_id = target WHERE organization_id = shared;

    UPDATE public.tasks SET organization_id = target
     WHERE organization_id = shared
       AND project_id::text IN (SELECT p.id FROM repomesh_projects.projects p WHERE p.organization_id = target);

    UPDATE public.agents
       SET organization_id = target,
           singleton_key = replace(singleton_key, shared::text, target::text)
     WHERE organization_id = shared;

    UPDATE repomesh_scan.repositories SET organization_id = target WHERE organization_id = shared;
    UPDATE public.projects SET organization_id = target WHERE organization_id = shared;
END $$;

-- 3) singleton_key 的唯一性：全局 -> 按组织
ALTER TABLE public.agents DROP CONSTRAINT IF EXISTS agents_singleton_key_key;
CREATE UNIQUE INDEX IF NOT EXISTS agents_org_singleton_key_key
    ON public.agents(organization_id, singleton_key)
 WHERE singleton_key IS NOT NULL;
