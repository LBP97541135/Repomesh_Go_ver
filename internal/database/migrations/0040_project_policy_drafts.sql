-- 0040: 项目监管策略草稿（前端「督导策略」弹窗的唯一落点）。
--
-- 为什么单开一张表，而不是塞进 public.agent_teams：
--   1) agent_teams 是**一仓一行**（ensureTeam 的 idempotency_key 是
--      assembly-team:{project}:{repo}），而监管意图是**一个项目一份**——
--      一份草稿没有"属于哪个仓库"这回事；
--   2) agent_teams.execution_mode 这一列**已经被占用**：assembly 往里写的是
--      'leader'（见 internal/assembly/assemble.go 的 ensureTeam），而读面
--      internal/agents/service.go 还在用它区分 'leader'/'server'。把
--      auto/supervised/manual_controlled 塞进同一列，就是把两套语义叠在一格里，
--      日后谁读错都不会有人发现。
-- 所以：草稿独立成表，project_id 就是主键（一个项目只有一份监管意图，
-- 改主意就是整份替换，没有第二份草稿可以被重复创建）。
CREATE TABLE IF NOT EXISTS public.project_policy_drafts (
    project_id uuid PRIMARY KEY,
    execution_mode text NOT NULL DEFAULT 'auto'
        CHECK (execution_mode IN ('auto', 'supervised', 'manual_controlled')),
    required_checkpoints jsonb NOT NULL DEFAULT '[]'::jsonb
        CHECK (jsonb_typeof(required_checkpoints) = 'array'),
    human_grants jsonb NOT NULL DEFAULT '[]'::jsonb
        CHECK (jsonb_typeof(human_grants) = 'array'),
    -- created_by 是**设它的那个账号**，不是被授权人（两者都只是 id）。
    created_by text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
-- frozen_at：首次物化时盖章，此后**改不动了**。
-- 为什么要有这一列：前端卡片写着「过了物化这一步，这个需求的监管策略就定死了」——
-- 而在此之前那句话只是界面上的说法，后端并没有任何东西阻止改。策略决定的是
-- 「这个需求会停在哪几处、谁能批」，同一份需求跑起来之后中途改强度，会让已经在
-- 旧强度下做的决策无从解释。所以定死这件事必须由存储层持有，不是界面的文案。
ALTER TABLE public.project_policy_drafts
    ADD COLUMN IF NOT EXISTS frozen_at timestamptz;

-- review_requests.decided_by 从 uuid 改成 text。
--
-- 为什么：这一列存的是**决策人的账号 id**（humancontrol.Decide 把会话账号写进去），
-- 而 repomesh_access.accounts.id 是 text —— 真实账号里既有 uuid 形状的
--（afd825a6-…），也有非 uuid 形状的（usr_e2e_…、acct-…）。uuid 列会在后一类账号
-- 决策时直接 22P02，而这条路径此前从未被走过（review_requests 恒 0 行、
-- 生产者不存在），所以一直没暴露。判据是「账号 id 的形状」，而账号 id 就是 text。
ALTER TABLE public.review_requests
    ALTER COLUMN decided_by TYPE text USING decided_by::text;
