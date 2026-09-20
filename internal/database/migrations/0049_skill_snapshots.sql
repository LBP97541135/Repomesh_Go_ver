-- 0049: 组织级技能快照（Skill Set Snapshot）。
--
-- 背景：参考 GOAI-infra-repomesh 的 SkillSnapshotRecord——一个组织在某次
-- 派工前锁定一组技能版本，运行中的任务继续使用它启动时的快照，新版本只影响
-- 后续任务。这保证 Agent 拿到的技能集在整个任务生命周期内不变，即使管理员
-- 中途晋升或回滚了新版本。
--
-- 设计：
--   organization_id  NULL = 全局快照（种子技能集）；非空 = 该空间私有快照。
--   versions         JSONB 数组，每项 {"skill_id","version","content_hash"}。
--   superseded_at    非 NULL 表示该快照已被新快照取代（保留历史，不删行）。
--   唯一约束：同一组织 + 同一版本集只能存在一份活跃快照。

CREATE TABLE IF NOT EXISTS public.skill_snapshots (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id uuid,
  versions jsonb NOT NULL DEFAULT '[]'::jsonb,
  created_at timestamptz NOT NULL DEFAULT now(),
  superseded_at timestamptz
);

CREATE UNIQUE INDEX IF NOT EXISTS skill_snapshots_org_versions_active
  ON public.skill_snapshots (COALESCE(organization_id, '00000000-0000-0000-0000-000000000000'::uuid), versions)
 WHERE superseded_at IS NULL;

CREATE INDEX IF NOT EXISTS skill_snapshots_org_active
  ON public.skill_snapshots (organization_id) WHERE superseded_at IS NULL;
