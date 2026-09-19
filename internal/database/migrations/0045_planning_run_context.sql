-- 0045_planning_run_context.sql — 规划派发的**上下文**（A1(d)：重排 v2）
--
-- 背景：收集窗开完之后的"重排"是一步真实的 agent 派发（发现链第 6 步），而它比
-- 前五步多两样输入：**上一版计划**（agent 是在 v1 上改，不是从零猜）与**受影响
-- 仓库集合 + 触发它的打断决策单**（重排的 v2 决策节点要指回那一跳）。
-- 这两样东西必须跟着"派发意图"一起落库：web 只写意图、coordinator 才派发，
-- 中间隔着进程边界，放进内存就等于"重启后丢一半"。
--
-- 形状：jsonb 对象，键按需（plan_id / upstream_ref / affected_repositories）。
-- 老行补 '{}'::jsonb —— 前五步不需要它，读面按"没有上下文"处理。

ALTER TABLE repomesh_issues.planning_runs
  ADD COLUMN IF NOT EXISTS context jsonb NOT NULL DEFAULT '{}'::jsonb;
