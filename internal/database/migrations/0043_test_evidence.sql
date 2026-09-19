-- 0043: 测试团队的**真实记录** —— task 单点验收 / 仓库集成 / 跨仓库联调与回归。
--
-- 背景（2026-09-20 线上实测）：双派工其实一直在跑（每条任务一个开发 run + 一个
-- test_agent run），测试 agent 也真的写了测试脚本、跑了、报告了命令与退出码 ——
-- 但平台**只留下一个退出码**（scm_commands 的 ci 行里 params 就一个 exitCode）。
-- 界面上的「测试组 · db-test」是写死的一行文案，永远显示「等待上游开发任务全部
-- 完成」；DAG 节点级的仓库集成、跨仓库联调与回归**一个字节都没有**。
--
-- 这张表是那些事实的落点。三种 kind：
--   task_single_point     —— 单条任务的单点验收（脚本、命令、退出码、结论）
--   repo_integration      —— 一个 DAG 节点（一个仓库）完成后，本仓库的集成验证
--   cross_repo_regression —— 跨仓库联调 + 回归（涉及多个仓库的 DAG 才有）
--
-- 只记真的发生过的事：脚本/命令/退出码/结论都取自 agent 写下的证据文件与 run 的
-- 真实退出，观测不到就不写行（界面因此显示"还没有记录"，而不是编一个通过）。
CREATE TABLE IF NOT EXISTS public.test_evidence (
    id            uuid PRIMARY KEY,
    project_id    uuid NOT NULL,
    issue_id      text NOT NULL,
    plan_id       uuid,
    -- 单点验收挂在具体任务上；节点级集成挂在仓库上（task_id 为空）。
    task_id       uuid,
    repository_id text NOT NULL DEFAULT '',
    kind          text NOT NULL CHECK (kind IN ('task_single_point', 'repo_integration', 'cross_repo_regression')),
    -- 证据本身：测试脚本路径、实际跑的命令、退出码、是否通过、一句话结论。
    script        text NOT NULL DEFAULT '',
    command       text NOT NULL DEFAULT '',
    exit_code     integer,
    passed        boolean NOT NULL DEFAULT false,
    summary       text NOT NULL DEFAULT '',
    -- 产出者：run id 与 agent 种类（审计用）。
    run_id        text NOT NULL DEFAULT '',
    producer      text NOT NULL DEFAULT '',
    created_at    timestamptz NOT NULL DEFAULT clock_timestamp()
);

CREATE INDEX IF NOT EXISTS idx_test_evidence_issue
    ON public.test_evidence (issue_id, kind, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_test_evidence_task
    ON public.test_evidence (task_id) WHERE task_id IS NOT NULL;
