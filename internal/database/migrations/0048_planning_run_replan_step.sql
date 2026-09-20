-- 0048_planning_run_replan_step.sql — 规划派发允许"重排步"（step=6）
--
-- 0041 给 planning_runs.step 写的是 CHECK (step IN (1, 2, 4))：当时只有发现链的
-- 前三步会派发（3/5 是人工门）。A1(d) 的重排是**第 6 步** —— 收集窗开完之后由
-- Leader 产出 v2。端到端测试直接撞上 23514 planning_runs_step_check，也就是说
-- 这条能力在线上**一跑就失败**（登记重排意图那一步就炸）。
--
-- 这里把 6 加进白名单；3/5 仍是人工门，不派发。
ALTER TABLE repomesh_issues.planning_runs DROP CONSTRAINT IF EXISTS planning_runs_step_check;
ALTER TABLE repomesh_issues.planning_runs
    ADD CONSTRAINT planning_runs_step_check CHECK (step IN (1, 2, 4, 6));