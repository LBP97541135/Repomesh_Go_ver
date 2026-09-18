-- Project unification: A-suite (repomesh_projects.projects) absorbs the B-suite
-- (public.projects). The B-suite tables keep existing for one migration cycle
-- but every writer now anchors on the A-suite project, so this migration
-- (1) gives the A-suite project an organization dimension (absorbing the
--     B-suite organization semantics),
-- (2) gives it a lifecycle status column for human control (pause/resume/
--     cancel previously wrote the B-suite status column),
-- (3) backfills decision_chain_nodes.project_id from the discovery state so
--     the decision chain is queryable per project, and
-- (4) documents the B-suite retirement on the old tables.
-- No data is deleted and no foreign key is dropped: public.projects and its
-- dependents (public.tasks, public.plans, public.agent_teams,
-- public.review_requests) are retired by convention, not by DDL.

-- 1) organization dimension on the surviving project table.
ALTER TABLE repomesh_projects.projects
    ADD COLUMN IF NOT EXISTS organization_id uuid;

DO $$
DECLARE
    default_org uuid;
BEGIN
    -- Same lazy default-organization convention as the discovery resolver:
    -- accounts have no organization binding (0003), so every existing project
    -- points at the single default organization.
    SELECT id INTO default_org FROM public.organizations ORDER BY created_at LIMIT 1;
    IF default_org IS NULL THEN
        INSERT INTO public.organizations (id, name)
        VALUES (gen_random_uuid(), '默认组织')
        RETURNING id INTO default_org;
    END IF;

    UPDATE repomesh_projects.projects
    SET organization_id = default_org
    WHERE organization_id IS NULL;

    ALTER TABLE repomesh_projects.projects
        ALTER COLUMN organization_id SET NOT NULL;
END $$;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'projects_organization_id_fkey'
          AND conrelid = 'repomesh_projects.projects'::regclass
    ) THEN
        ALTER TABLE repomesh_projects.projects
            ADD CONSTRAINT projects_organization_id_fkey
            FOREIGN KEY (organization_id) REFERENCES public.organizations(id);
    END IF;
END $$;

CREATE INDEX IF NOT EXISTS projects_organization_id
    ON repomesh_projects.projects(organization_id, id);

-- 2) lifecycle status for human control (pause/resume/cancel project).
ALTER TABLE repomesh_projects.projects
    ADD COLUMN IF NOT EXISTS status text NOT NULL DEFAULT 'active'
    CHECK (status IN ('active', 'paused', 'cancelled'));

-- 3) decision-chain nodes adopt the project context from the discovery state.
-- decision_chain_nodes.project_id is uuid while issue_discoveries.project_id is
-- a text A-suite id, so the physical FK from the original analysis is not
-- possible; the association is logical (documented here) and guarded by the
-- uuid shape check before the cast. Both sides store the normalized
-- requirement text (strings.Fields joined by single spaces).
UPDATE public.decision_chain_nodes dn
SET project_id = (id_.project_id)::uuid
FROM repomesh_issues.issue_discoveries id_
WHERE dn.project_id IS NULL
  AND dn.requirement_text = id_.requirement_text
  AND id_.project_id ~ '^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$';

COMMENT ON TABLE public.projects IS
    'RETIRED (0030): superseded by repomesh_projects.projects. Kept for one migration cycle; writers must not anchor here.';
