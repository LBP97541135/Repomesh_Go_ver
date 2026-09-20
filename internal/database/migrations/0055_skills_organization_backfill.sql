-- 0055: 回填 public.skills 的空间归属 —— 0039 漏了这一步（0053/0054 让给了 hitl_mode/agent_project_scope）
--
-- 背景（0039 的原话）：`public.skills` 此前是全局一份，公有部署下任何登录账号都能
-- 看到别人的技能，甚至能按名字覆盖别人的技能。0039 的解法是加 `organization_id`，
-- 并规定 **NULL = 全局种子技能（系统自带，所有人可见）**。
--
-- 但 0039 只回填了扫描目录（`repomesh_scan.repositories`），**没有回填 skills 自己**。
-- 后果：任何在 0039 之前注册的技能，`organization_id` 会一直留在 NULL —— 于是它从
-- "某个账号的私有技能"变成了"所有人可见的全局种子"。这不是理论风险：0039 的注释里
-- 还记着当时的 `RegisterSkill` 用的是 `ON CONFLICT (name) DO UPDATE`，同名技能本就会
-- 被后来者覆盖。
--
-- 2026-09-20 线上核查：本部署 `public.skills` 共 15 行，全部 `created_by='system-seed'`
-- 且 `organization_id IS NULL`；`created_by <> 'system-seed'` 的行 0 条 —— 也就是**这个
-- 库恰好没有踩到**（从来没人注册过技能）。所以本迁移在本部署上是 no-op，它的价值在
-- 于修掉这条线上的其它部署（以及防止将来有人拿旧快照恢复出一个带历史技能的库）。
--
-- 策略与 0039 一致，分两步，都幂等、都只动"非种子的 NULL 行"：
--   1. `created_by` 能对上某个账号 → 归到那个账号的空间（精确归属，优先）。
--   2. 对不上的（历史遗留的 agent id、已删除账号、任意字符串）→ 归到唯一的活跃空间，
--      但**仅当全库只有一个活跃空间时**才这么做（0039 是在"只有一个真人账号"的前提下
--      这么干的；多空间部署里猜错会把技能塞进别人空间，比留 NULL 更糟，所以留 NULL）。
-- `system-seed` 的行一行都不动：它们本来就该是全局种子。
--
-- 复查（本迁移执行后应当返回 0 行，除非是多空间部署且存在无主技能）：
--   SELECT id, name, created_by FROM public.skills
--    WHERE organization_id IS NULL AND created_by <> 'system-seed';

UPDATE public.skills s
   SET organization_id = a.organization_id
  FROM repomesh_access.accounts a
 WHERE s.organization_id IS NULL
   AND s.created_by <> 'system-seed'
   AND a.organization_id IS NOT NULL
   AND a.id::text = s.created_by;

UPDATE public.skills s
   SET organization_id = (
        SELECT a.organization_id
          FROM repomesh_access.accounts a
         WHERE a.disabled = false AND a.organization_id IS NOT NULL
         ORDER BY a.id
         LIMIT 1)
 WHERE s.organization_id IS NULL
   AND s.created_by <> 'system-seed'
   AND (
        SELECT count(DISTINCT a.organization_id)
          FROM repomesh_access.accounts a
         WHERE a.disabled = false AND a.organization_id IS NOT NULL
       ) = 1;
