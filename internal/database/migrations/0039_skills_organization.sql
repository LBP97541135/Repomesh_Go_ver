-- 0039: 技能库按空间（组织）隔离。
--
-- 背景：public.skills 此前是**全局一份**（skills_name_key UNIQUE(name)）。
-- 公有部署（一账号一空间）下，任何登录账号都能看到别人的技能，并且能按名字
-- 覆盖别人的技能（RegisterSkill 是 ON CONFLICT (name) DO UPDATE）。
--
-- 设计：organization_id 为 NULL = **全局种子技能**（系统自带，所有人可见）；
-- 非空 = 该空间的私有技能。名字唯一性随之改成两级：
--   全局种子之间唯一（organization_id IS NULL）
--   同一空间内唯一（organization_id, name）
-- 这样既允许两个空间各有一把同名技能，也不会让种子技能被重复插入。
ALTER TABLE public.skills ADD COLUMN IF NOT EXISTS organization_id uuid;

ALTER TABLE public.skills DROP CONSTRAINT IF EXISTS skills_name_key;

CREATE UNIQUE INDEX IF NOT EXISTS skills_global_name_key
    ON public.skills(name)
 WHERE organization_id IS NULL;

CREATE UNIQUE INDEX IF NOT EXISTS skills_org_name_key
    ON public.skills(organization_id, name)
 WHERE organization_id IS NOT NULL;

-- 扫描目录（repomesh_scan.repositories）同样按空间隔离。
-- 该表**本来就有** organization_id 列（可空），但从来没人写过——所以读面一直是
-- 全库可见。这里把既有行归属到"当前唯一活跃账号"的空间（与 0037 同策略：
-- 本部署只有一个真人账号，历史数据都属于它）。
UPDATE repomesh_scan.repositories r
   SET organization_id = (
        SELECT a.organization_id
          FROM repomesh_access.accounts a
         WHERE a.disabled = false AND a.organization_id IS NOT NULL
         ORDER BY a.id
         LIMIT 1)
 WHERE r.organization_id IS NULL;
