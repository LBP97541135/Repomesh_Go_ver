-- 0038: 交付运行记录它要交付的仓库（C 修复，2026-09-19）。
--
-- 背景：交付脚本此前从部署级缓存文件 /opt/repomesh/workspaces/.gh-token 取
-- installation token，而那个文件由 repomesh-gh-token.timer 用
-- `-repo LBP97541135/repomesh-e2e-client` 写死刷新——token 是 installation
-- 限定的，别人把 App 装到自己账号/组织上之后，流水线照样拿这条旧 token 去
-- push，必然 403。修法是**按仓库现场铸**：host-executor 在启动运行前用部署
-- 自己的 App 私钥为该仓库解析 installation 并铸一个短期 token，只以环境变量
-- 交给子进程。
--
-- 因此运行记录必须带上"这次要交付哪个仓库"这一事实（此前只存在于命令字符串
-- 里的 R=... 一行，靠解析字符串取仓库是脆的）。列可空默认 ''：测试 run 与
-- 历史行不带仓库，executor 对空值跳过铸 token。
ALTER TABLE repomesh_execution.agent_runs
    ADD COLUMN IF NOT EXISTS repo_full_name text NOT NULL DEFAULT '';
