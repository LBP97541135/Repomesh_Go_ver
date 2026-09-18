-- 0021_replan_protocol.sql — 异常重规划协议增量（B11，协议 §2 步骤 3-6）
-- 基座 = 0009/0010 的平台核心与任务主线（plans/plan_steps/tasks/task_assignments）。
-- 本迁移只加重规划必需的增量，不改既有列：
--   * tasks.task_uid：跨计划版本的稳定任务身份（显式 uid + 仓库名/标题兜底匹配）
--   * tasks.plan_id：快照修订的作用域（计划 ↔ 任务成员关系）
--   * plans.requirement_key：与决策链同款检索键
--   * plans.replan_state：收集窗状态（'' | deprecated）
-- 业务 SQL 一律 public. 全限定（search_path=pg_catalog 约定）。

ALTER TABLE public.tasks ADD COLUMN IF NOT EXISTS task_uid text NOT NULL DEFAULT '';
ALTER TABLE public.tasks ADD COLUMN IF NOT EXISTS plan_id uuid;
ALTER TABLE public.plans ADD COLUMN IF NOT EXISTS requirement_key text NOT NULL DEFAULT '';
ALTER TABLE public.plans ADD COLUMN IF NOT EXISTS replan_state text NOT NULL DEFAULT '';

-- 唯一性按项目分域：不同项目可复用同一 uid 惯例而不互撞（审查 #4）。
CREATE UNIQUE INDEX IF NOT EXISTS uq_tasks_task_uid
  ON public.tasks (project_id, task_uid)
  WHERE task_uid <> '' AND status <> 'superseded';
CREATE INDEX IF NOT EXISTS idx_tasks_plan_id
  ON public.tasks (plan_id);
CREATE INDEX IF NOT EXISTS idx_plans_requirement_key
  ON public.plans (requirement_key);
