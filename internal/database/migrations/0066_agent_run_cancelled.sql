-- 0066: 排队中、**从未启动**的 run 需要一个「人工取消」的终态，而不是被编一个退出码。
--
-- 背景（2026-09-22 线上实测，用户要求"停掉这个任务"）：
--   停止一个 issue 时，库里既有已经跑起来的 run，也有**刚派发、还在 pending** 的 run。
--   前者可以如实置 'killed'（进程真的被杀了，退出码是真的）；后者**一个进程都没起过**，
--   它既没有退出码，也没有"被杀"这回事 —— 而 lifecycle CHECK 当时只允许两种收尾形态：
--     · 'exited' / 'killed'：都断言 exit_code IS NOT NULL —— 那是编数据；
--     · 'lost'：断言"我们确认它没了"，但这条 run 从来没存在过，也谈不上丢；
--     · 'failed_launch'：断言"启动失败" —— 它没失败，是被人取消的。
--   四个状态没有一个说的是实话，于是人工停止只能**把 pending 行留在原地**：任务已经
--   是 failed，这些 run 却永远挂着"排队中"，读的人分不清"还在等"和"已经不要了"。
--
-- 加值修法（不删任何既有取值，老数据一行不动）：
--   · state 加 'cancelled'；
--   · lifecycle 给 'cancelled' 一条自己的形状：**从未启动**（started_at IS NULL）、
--     **没有退出码**（exit_code IS NULL）、**有确认时刻**（exited_at NOT NULL）。
--     这三条正好把"没跑过"和"跑过但结局未知"区分开 —— 前者是 cancelled，后者是 lost。
ALTER TABLE repomesh_execution.agent_runs
    DROP CONSTRAINT agent_runs_state_check;
ALTER TABLE repomesh_execution.agent_runs
    ADD CONSTRAINT agent_runs_state_check
    CHECK (state IN ('pending','running','exited','killed','failed_launch','lost','cancelled'));

ALTER TABLE repomesh_execution.agent_runs
    DROP CONSTRAINT agent_runs_lifecycle;
ALTER TABLE repomesh_execution.agent_runs
    ADD CONSTRAINT agent_runs_lifecycle CHECK (
        (state = 'running' AND pid IS NOT NULL AND started_at IS NOT NULL)
        OR (state IN ('exited','killed') AND exit_code IS NOT NULL AND exited_at IS NOT NULL)
        OR (state = 'pending' AND pid IS NULL)
        OR (state = 'failed_launch' AND started_at IS NULL)
        -- 结局未知：退出码留空是**事实**，不是遗漏。
        OR (state = 'lost' AND exited_at IS NOT NULL)
        -- 人工取消：从未启动、没有退出码 —— 两处留空同样是事实。
        OR (state = 'cancelled' AND started_at IS NULL AND exit_code IS NULL AND exited_at IS NOT NULL)
    );

-- 证据链上也要留下这一跳：kind 加 'agent_cancelled'，与 'agent_exited'/'agent_lost' 并列。
-- 回看一条 attempt 时，"跑完了 / 丢了 / 压根没让跑"一眼可分。
ALTER TABLE repomesh_execution.attempt_events
    DROP CONSTRAINT attempt_events_kind_allowed;
ALTER TABLE repomesh_execution.attempt_events
    ADD CONSTRAINT attempt_events_kind_allowed
    CHECK (kind IN ('reserved','environment_prepared','launch_verified','started',
                    'agent_started','agent_exited','agent_lost','agent_cancelled',
                    'stop_requested','stopped','failed','observed_unknown'));
