-- 0035: 智能体级配置（2026-09-19 用户裁定 A：每个智能体可以有预设的提示词）。
--
-- 控制台智能体页的人工配置落点：预设提示词 + 该 worker 用哪个 CLI 工具。
-- 加列而不是新建表：这两项是「一个智能体一份」的属性，与 agents 行同生命周期；
-- 单独建表只会多一次 join、多一类孤儿清理，没有收益。
--
-- cli_kind 空串 = 未指定，此时沿用项目级 agent_settings.agent_kind（0034）与
-- 部署级默认——三层优先级：智能体 > 项目 > 部署。
ALTER TABLE public.agents ADD COLUMN IF NOT EXISTS prompt text NOT NULL DEFAULT '';
ALTER TABLE public.agents ADD COLUMN IF NOT EXISTS cli_kind text NOT NULL DEFAULT '';
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'agents_cli_kind_check'
    ) THEN
        ALTER TABLE public.agents
            ADD CONSTRAINT agents_cli_kind_check
            CHECK (cli_kind IN ('', 'codex_cli', 'claude_cli'));
    END IF;
END $$;
