-- 0036: 账号的组织归属（多账户，2026-09-19 用户裁定「每个人都可以登录自己的账号，
-- 我的服务器统一处理」）。
--
-- 背景：repomesh_access.accounts 此前**没有组织列**，而名册查询曾按
-- `(SELECT organization_id FROM public.users WHERE id=$1)` 裁剪——`public.users`
-- 是**空表**（真实账号在 accounts），于是名册恒返回 0 行。今天已把那个假守卫去掉，
-- 但那只是绕开；**组织归属本来就该落在账号上**，否则服务端没有"这个人属于哪个组织"
-- 的事实，无法统一裁剪数据。
--
-- 本部署只有一个组织（默认组织），所以回填为它；多组织上线时改这里的分配策略即可。
ALTER TABLE repomesh_access.accounts
    ADD COLUMN IF NOT EXISTS organization_id uuid;

UPDATE repomesh_access.accounts
   SET organization_id = (SELECT id FROM public.organizations ORDER BY created_at LIMIT 1)
 WHERE organization_id IS NULL;

-- e2e 测试造出来的 41 个 `e2e-owner` 账号停用：它们不是真人，留在启用态会污染
-- 账号列表与计数（实测 accounts 42 行里 41 行是它）。停用**可逆**（disabled 是布尔列），
-- 不做删除——硬删会牵动引用它们的历史行。
UPDATE repomesh_access.accounts
   SET disabled = true
 WHERE display_name = 'e2e-owner';
