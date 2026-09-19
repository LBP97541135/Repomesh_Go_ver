-- 0034: 项目级智能体执行配置。派发缝（coordinator ledger）按项目读取这里的
-- CLI 种类与模型，覆盖部署级默认值，让"项目管理"真正控制智能体执行。
CREATE TABLE IF NOT EXISTS repomesh_projects.agent_settings (
    project_id text PRIMARY KEY REFERENCES repomesh_projects.projects(id),
    agent_kind text NOT NULL DEFAULT 'codex_cli' CHECK (agent_kind IN ('codex_cli', 'claude_cli')),
    model text NOT NULL DEFAULT 'MiniMax-M2' CHECK (length(model) BETWEEN 1 AND 64),
    updated_at timestamptz NOT NULL DEFAULT now()
);
