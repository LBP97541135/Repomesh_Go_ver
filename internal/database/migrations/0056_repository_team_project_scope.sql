-- 0056: 团队层按 (项目, 仓库) 隔离 —— repository_teams 的主键从「仓库」改成「项目 + 仓库」。
--
-- 为什么改（2026-09-20）：
--   业务链是 账号 → 项目 → issue → 仓库，同一份仓库允许挂到多个项目上。但
--   `public.repository_teams` 的主键是 `repository_id`（扫描侧 id），语义是
--   「全局一个仓库一支队」。于是：
--     · `EnsureForRepository` 的 EXISTS 判定只看仓库 → 第二个项目判定「已有队」，
--       **静默跳过**（现象不是"共用一支"，是"第二个项目压根没有队"，且不报错）；
--     · `repository_team_workers` 也只按仓库挂，且 `resource_name` 是全局唯一；
--     · `remotePrefix()` 只喂仓库 id → 两个项目算出**同一个远端队名**。
--   编制（public.agents）已在 0054 按项目隔离；这一步把「队」这一层补齐，
--   否则「人」按项目分、「队」按仓库分，仍然会串。
--
-- 存量数据（重要）：
--   2026-09-20 13:49（31715538「接入即自动建队」）到该功能被默认关闭之间，
--   **生产上真的建过队**（main.go 注释记录：42 支队 / 124 个 worker），所以本迁移
--   必须带回填，不能按空表处理；远端 AgentTeams 也**真的存在** `repomesh-r-*` 资源。
--
-- 做法（幂等，可重跑；不改任何既有迁移）：
--   1) 两表加 `project_id`；
--   2) 沿用 `repoTeamResolutionQuery` 的 URL 对齐规则（方向相反）判定归属，
--      **只有唯一命中一个项目时才回填**；
--   3) 判不了的行（0 个或多个候选）**先存档再移出主表** —— 不编造归属，
--      存档表里留着 `agentteams_team_name`，远端资源可据此人工清理；
--   4) 主键 / 外键 / 唯一索引全部改成含 `project_id`。
--
-- 远端队名策略（2026-09-20 用户裁定：存量优先、零孤儿）：
--   **已存在的队名原样保留**，不给存量改名。代码侧同步改为"队名一律从
--   `agentteams_team_name` 读，不再自己重算"，因此只有**新**建的队才会用到
--   含项目的命名。这样无论远端是否真建过队，都不会产生孤儿。
--
-- 已知限制（如实记录，不在本迁移解决）：
--   · `repository_teams.repository_id` 是**扫描侧** id，而
--     `repomesh_projects.project_repositories.repository_id` 是**项目侧** `repo_…`，
--     两个 id 空间不同 → **无法**建 (project_id, repository_id) 到 project_repositories
--     的复合外键。这里只对 `project_id` 建外键。
--   · `agentteams_team_name` 仍保持全局唯一（新命名含项目后天然唯一，存量名也不冲突）。

SET LOCAL search_path = public, pg_catalog;

-- ---------------------------------------------------------------------------
-- 1) 存档表：装"判不了项目归属"的旧行
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS public.repository_teams_unresolved (
    repository_id        text PRIMARY KEY,
    agentteams_team_name text NOT NULL,
    leader_id            uuid,
    leader_resource_name text,
    runtime_status       text,
    roster_revision      bigint,
    worker_count         integer NOT NULL DEFAULT 0,
    created_at           timestamptz,
    updated_at           timestamptz,
    archived_at          timestamptz NOT NULL DEFAULT now(),
    reason               text NOT NULL
);

COMMENT ON TABLE public.repository_teams_unresolved IS
  '0056 迁移时无法唯一判定项目归属的旧团队行。远端 AgentTeams 可能仍有对应真实资源（名为 agentteams_team_name），保留此表供人工清理；只增不改。';

-- ---------------------------------------------------------------------------
-- 2) 加列
-- ---------------------------------------------------------------------------
ALTER TABLE public.repository_teams        ADD COLUMN IF NOT EXISTS project_id uuid;
ALTER TABLE public.repository_team_workers ADD COLUMN IF NOT EXISTS project_id uuid;

-- ---------------------------------------------------------------------------
-- 3) 判定归属：扫描侧仓库 → 项目（沿用 repoTeamResolutionQuery 的 URL 对齐规则，反向）
--    candidates = 命中的**不同项目**个数；min() 忽略 NULL，只在唯一命中时才有值。
-- ---------------------------------------------------------------------------
CREATE TEMP TABLE team_project_resolved ON COMMIT DROP AS
SELECT t.repository_id           AS scan_side,
       count(DISTINCT x.project_id) AS candidates,
       min(x.project_id)         AS project_id
FROM public.repository_teams t
LEFT JOIN LATERAL (
    SELECT pr.project_id
    FROM repomesh_scan.repositories scan
    JOIN repomesh_access.accounts a  ON a.organization_id = scan.organization_id
    JOIN repomesh_projects.projects p ON p.owner = a.id
    JOIN repomesh_projects.project_repositories pr ON pr.project_id = p.id
    JOIN repomesh_projects.repositories r ON r.id = pr.repository_id
    WHERE scan.id = t.repository_id
      AND lower(regexp_replace(rtrim(scan.url, '/'), '\.git$', '')) IN (
            lower('https://' || r.host || '/' || r.owner || '/' || r.name),
            lower('http://'  || r.host || '/' || r.owner || '/' || r.name),
            lower('git@'     || r.host || ':' || r.owner || '/' || r.name))
) x ON true
GROUP BY t.repository_id;

-- ---------------------------------------------------------------------------
-- 4) 判不了的行：先存档，再移出主表（否则 project_id 没法设 NOT NULL）
-- ---------------------------------------------------------------------------
INSERT INTO public.repository_teams_unresolved
    (repository_id, agentteams_team_name, leader_id, leader_resource_name,
     runtime_status, roster_revision, worker_count, created_at, updated_at, reason)
SELECT t.repository_id, t.agentteams_team_name, t.leader_id, t.leader_resource_name,
       t.runtime_status, t.roster_revision,
       (SELECT count(*) FROM public.repository_team_workers w WHERE w.repository_id = t.repository_id),
       t.created_at, t.updated_at,
       CASE WHEN r.candidates = 0
            THEN '找不到匹配的项目（扫描 URL 对不上，或项目/仓库登记已不存在）'
            ELSE '匹配到 ' || r.candidates || ' 个项目，无法唯一判定归属'
       END
FROM public.repository_teams t
JOIN team_project_resolved r ON r.scan_side = t.repository_id
WHERE r.candidates <> 1
ON CONFLICT (repository_id) DO NOTHING;

DELETE FROM public.repository_team_workers w
USING team_project_resolved r
WHERE w.repository_id = r.scan_side
  AND r.candidates <> 1;

DELETE FROM public.repository_teams t
USING team_project_resolved r
WHERE t.repository_id = r.scan_side
  AND r.candidates <> 1;

-- ---------------------------------------------------------------------------
-- 5) 回填
-- ---------------------------------------------------------------------------
UPDATE public.repository_teams t
SET project_id = r.project_id
FROM team_project_resolved r
WHERE r.scan_side = t.repository_id
  AND r.candidates = 1
  AND t.project_id IS DISTINCT FROM r.project_id;

UPDATE public.repository_team_workers w
SET project_id = t.project_id
FROM public.repository_teams t
WHERE w.repository_id = t.repository_id
  AND w.project_id IS DISTINCT FROM t.project_id;

-- ---------------------------------------------------------------------------
-- 6) 约束改造：主键 / 外键 / 唯一索引，全部改成含 project_id
-- ---------------------------------------------------------------------------
-- 6a) 子表先摘掉对旧主键的外键（顺序不能反）
ALTER TABLE public.repository_team_workers
    DROP CONSTRAINT IF EXISTS repository_team_workers_repository_id_fkey;

-- 6b) 两列转 NOT NULL（此时判不了的行已移出，不会再撞 NULL）
ALTER TABLE public.repository_teams        ALTER COLUMN project_id SET NOT NULL;
ALTER TABLE public.repository_team_workers ALTER COLUMN project_id SET NOT NULL;

-- 6c) 主表主键：repository_id → (project_id, repository_id)
ALTER TABLE public.repository_teams DROP CONSTRAINT IF EXISTS repository_teams_pkey;
ALTER TABLE public.repository_teams
    ADD CONSTRAINT repository_teams_pkey PRIMARY KEY (project_id, repository_id);

-- 6d) 主表 → 项目（只对 project_id 建外键，理由见文件头「已知限制」）
ALTER TABLE public.repository_teams
    DROP CONSTRAINT IF EXISTS repository_teams_project_fkey;
ALTER TABLE public.repository_teams
    ADD CONSTRAINT repository_teams_project_fkey
    FOREIGN KEY (project_id) REFERENCES repomesh_projects.projects(id) ON DELETE CASCADE;

-- 6e) 子表 → 主表（复合外键）
--     先 DROP 再 ADD：`ADD CONSTRAINT` 没有 IF NOT EXISTS，不先摘掉就不可重跑。
ALTER TABLE public.repository_team_workers
    DROP CONSTRAINT IF EXISTS repository_team_workers_team_fkey;
ALTER TABLE public.repository_team_workers
    ADD CONSTRAINT repository_team_workers_team_fkey
    FOREIGN KEY (project_id, repository_id)
    REFERENCES public.repository_teams(project_id, repository_id) ON DELETE CASCADE;

-- 6f) 子表唯一性全部改成含项目（原先是全局/按仓库）
ALTER TABLE public.repository_team_workers
    DROP CONSTRAINT IF EXISTS repository_team_workers_resource_name_key;
ALTER TABLE public.repository_team_workers
    DROP CONSTRAINT IF EXISTS repository_team_workers_project_resource_name_key;
ALTER TABLE public.repository_team_workers
    ADD CONSTRAINT repository_team_workers_project_resource_name_key
    UNIQUE (project_id, resource_name);

ALTER TABLE public.repository_team_workers
    DROP CONSTRAINT IF EXISTS repository_team_workers_repository_id_creation_sequence_key;
ALTER TABLE public.repository_team_workers
    DROP CONSTRAINT IF EXISTS repository_team_workers_project_creation_sequence_key;
ALTER TABLE public.repository_team_workers
    ADD CONSTRAINT repository_team_workers_project_creation_sequence_key
    UNIQUE (project_id, repository_id, creation_sequence);

DROP INDEX IF EXISTS public.uq_repository_team_active_worker_order;
CREATE UNIQUE INDEX uq_repository_team_active_worker_order
    ON public.repository_team_workers(project_id, repository_id, display_order)
    WHERE status = 'active';

-- 6g) 「这个仓库(不限项目)有没有队」的查法变了，补一个按仓库的索引
CREATE INDEX IF NOT EXISTS idx_repository_teams_repository
    ON public.repository_teams(repository_id);

-- ---------------------------------------------------------------------------
-- 7) 注释
-- ---------------------------------------------------------------------------
COMMENT ON COLUMN public.repository_teams.project_id IS
  '0056：团队归属的项目。与 repository_id 共同构成主键，保证同一仓库挂多个项目时各有一支队。';
COMMENT ON COLUMN public.repository_team_workers.project_id IS
  '0056：与所属团队的项目一致；唯一性与排序都在 (project_id, repository_id) 内生效。';
COMMENT ON COLUMN public.repository_teams.agentteams_team_name IS
  '远端 AgentTeams 队名。**以本列为准**，代码不再自行重算（0056）：存量队名保持原样（repomesh-r-<仓库hash>），新建队才用含项目的命名，避免改名产生孤儿。';
