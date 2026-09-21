-- 0063: agent run 需要一个「结局未知」的如实状态，而不是被编一个退出码。
--
-- 背景（2026-09-21 线上实测，用户报"任务一直执行不完"）：
--   host-executor 监督子进程靠的是**本进程里等 `cmd.Wait()`**。executor 一旦被重启
--   （部署 / systemd restart / OOM），等待随进程消失，那条 run 就永远停在 'running'：
--   任务交不回经理门（"交回经理门"只写在退出路径上），attempt 也永远占着并发额度。
--   实测 run_dag_c4aba7a93a72d817c5d0（saleor-app-template 的开发 run）11:05:48 起跑，
--   pid 早已不存在，两小时后台账还写着 running；库里同时有 36 条 attempt 停在 running。
--
-- 修法是启动时扫一遍、把孤儿 run 收尾。收尾时必须**如实**：我们知道的是"进程没了"，
-- 不知道的是"它退出码是多少"。所以：
--   · state 用 'lost'（不是 'exited' 也不是 'killed'，那两个都断言了退出码）；
--   · exit_code 允许为空 —— 空就是空，不拿 -1 冒充；
--   · exited_at 必填（"什么时候确认它没了"是已知事实）。
--
-- 放宽两处 CHECK（都是**加值**，不删任何既有取值，老数据一行不动）：
ALTER TABLE repomesh_execution.agent_runs
    DROP CONSTRAINT agent_runs_state_check;
ALTER TABLE repomesh_execution.agent_runs
    ADD CONSTRAINT agent_runs_state_check
    CHECK (state IN ('pending','running','exited','killed','failed_launch','lost'));

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
    );

-- 证据链上也要留下这一跳：kind 加 'agent_lost'，与 'agent_exited' 并列。
-- 这样回看一条 attempt 时，"这条 run 是正常退出还是丢了"一眼可分。
ALTER TABLE repomesh_execution.attempt_events
    DROP CONSTRAINT attempt_events_kind_allowed;
ALTER TABLE repomesh_execution.attempt_events
    ADD CONSTRAINT attempt_events_kind_allowed
    CHECK (kind IN ('reserved','environment_prepared','launch_verified','started',
                    'agent_started','agent_exited','agent_lost',
                    'stop_requested','stopped','failed','observed_unknown'));
