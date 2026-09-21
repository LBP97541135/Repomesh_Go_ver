-- 0064: PR 的**合并方式**要可选 —— ai 自动模式可以自动合并，也可以留给人工逐条审。
--
-- 用户 2026-09-21 原话："我们能不能把 pr 合并也做出可选择项目，ai 自动模式自动合并，
-- 也可以选择人工审核"。也就是说：合并不再是"永远人工"或"永远自动"的硬编码，
-- 而是**建单时的一个选择**，与「自动托管 / 人工参与审计」并列。
--
-- 为什么默认 manual（人工）：
--   · 合并是整条链上**唯一的外部副作用**（真动用户的仓库、真进主分支）；
--   · 缺省必须是最保守的那一侧 —— 没有明说要自动合并的，就不替任何人合。
--   · 想全自动就在建单时选「自动合并」，那是显式的、可追溯的授权。
--
-- 为什么落在 issues 表而不是复用 delivery_policies.auto_merge：
--   那张表是**按仓库**的长期策略（organization_id + repository_id，目前 0 行），
--   而这次要的是**按需求**的一次性选择（同一个人可能这条要自动、那条要人审）。
--   两者粒度不同，混用会让"这条需求到底会不会自动合并"变得说不清。
ALTER TABLE repomesh_issues.issues
    ADD COLUMN IF NOT EXISTS merge_mode text NOT NULL DEFAULT 'manual';

ALTER TABLE repomesh_issues.issues
    DROP CONSTRAINT IF EXISTS issues_merge_mode_check;
ALTER TABLE repomesh_issues.issues
    ADD CONSTRAINT issues_merge_mode_check CHECK (merge_mode IN ('auto', 'manual'));
