-- 0041: 规划期也走真实 agent —— 需求分析/候选评分/生成计划由角色 agent 产出。
--
-- 背景（2026-09-20 审计）：发现链五步此前**全部由 Go 代码算**（① 词表判定、
-- ② 规则召回、③ 规则分档、④ 模板拼任务），`created_by_agent_id` 只是个图章 ——
-- agent 一行代码都没执行。这与 infra 的 Governed AgentTeams Flow 相悖：
-- 那里 Scope 由 Organization Leader 提、Specification 与 Task DAG 由 Repository
-- Leader 写，后端只做**校验、门禁与记录**。
--
-- 本迁移只做两件事：
--   1) 允许 agent_runs 记录规划期的 run（agent_kind 增 'planning_agent'）；
--   2) 建一张审计表，把「这一步是谁产出的、用的哪把技能、原始产物是什么、
--      为什么没通过校验」如实留下来 —— 规划结论从此可追溯，不再是一个匿名 JSON。
ALTER TABLE repomesh_execution.agent_runs DROP CONSTRAINT IF EXISTS agent_runs_agent_kind_check;
ALTER TABLE repomesh_execution.agent_runs
    ADD CONSTRAINT agent_runs_agent_kind_check
    CHECK (agent_kind IN ('claude_cli', 'codex_cli', 'test_agent', 'planning_agent'));

CREATE TABLE IF NOT EXISTS repomesh_issues.planning_runs (
    id uuid PRIMARY KEY,
    issue_id text NOT NULL,
    -- step: 1=需求分析 2=候选评分 4=生成计划（3/5 是人工门，不派发）
    step integer NOT NULL CHECK (step IN (1, 2, 4)),
    -- 产出角色（infra 的角色表：organization_leader / repository_leader）
    role text NOT NULL,
    skill_id text NOT NULL DEFAULT '',
    skill_version text NOT NULL DEFAULT '',
    attempt_id text,
    run_id text,
    -- pending=已派发未收 / succeeded=产物已校验入库 / failed=进程或产物不合格
    state text NOT NULL DEFAULT 'pending'
        CHECK (state IN ('pending', 'succeeded', 'failed')),
    -- 原始产物原样留存（不裁剪）：审计要的是 agent 真正写了什么
    artifact jsonb,
    error text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    collected_at timestamptz
);
CREATE INDEX IF NOT EXISTS planning_runs_issue_step
    ON repomesh_issues.planning_runs (issue_id, step, created_at DESC);
CREATE INDEX IF NOT EXISTS planning_runs_pending
    ON repomesh_issues.planning_runs (state) WHERE state = 'pending';