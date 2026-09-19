-- Preserve legacy task payloads, but give repository identity a relational
-- representation. Invalid historical tasks stay intact and unbound; dispatch
-- only consumes bound rows. Never repair an invalid task by enlarging a scope.
ALTER TABLE public.plans ADD COLUMN issue_id text;

WITH legacy_plan_issues AS (
    SELECT p.id,min(i.id) AS issue_id
    FROM public.plans p
    JOIN repomesh_issues.issue_discoveries d ON p.id::text=d.plan->>'plan_id'
    JOIN repomesh_issues.issues i ON i.id=d.issue_id AND i.project_id=d.project_id
    WHERE p.project_id::text=i.project_id
    GROUP BY p.id HAVING count(DISTINCT i.id)=1
)
UPDATE public.plans p SET issue_id=matched.issue_id
FROM legacy_plan_issues matched WHERE matched.id=p.id;

ALTER TABLE public.plans ADD CONSTRAINT plans_issue_fk
    FOREIGN KEY (issue_id) REFERENCES repomesh_issues.issues(id);

CREATE TABLE public.task_repository_scopes (
    task_id uuid PRIMARY KEY REFERENCES public.tasks(id) ON DELETE CASCADE,
    project_id text NOT NULL,
    repository_id text NOT NULL,
    issue_id text,
    FOREIGN KEY (project_id, repository_id)
        REFERENCES repomesh_projects.project_repositories(project_id, repository_id),
    FOREIGN KEY (project_id, issue_id)
        REFERENCES repomesh_issues.issues(project_id, id),
    FOREIGN KEY (issue_id, repository_id)
        REFERENCES repomesh_issues.issue_repository_scope(issue_id, repository_id)
);

-- Only unambiguous, already valid identities are backfilled. Names remain in
-- tasks.repository_id for old consumers; this table always stores repo_ IDs.
INSERT INTO public.task_repository_scopes(task_id, project_id, repository_id, issue_id)
SELECT t.id, p.id, matched.id, COALESCE(NULLIF(t.source_ref->>'issueId',''), plan.issue_id)
FROM public.tasks t
JOIN repomesh_projects.projects p ON p.id=t.project_id::text AND p.organization_id=t.organization_id
LEFT JOIN public.plans plan ON plan.id=t.plan_id AND plan.project_id=t.project_id
JOIN LATERAL (
    SELECT min(r.id) AS id FROM repomesh_projects.project_repositories pr
    JOIN repomesh_projects.repositories r ON r.id=pr.repository_id
    WHERE pr.project_id=p.id AND t.repository_id IN (r.id, r.owner || '/' || r.name, r.name)
    HAVING count(*)=1
) matched ON true
WHERE (t.plan_id IS NULL OR plan.id IS NOT NULL)
  AND (plan.issue_id IS NULL OR NULLIF(t.source_ref->>'issueId','') IS NULL
       OR plan.issue_id=t.source_ref->>'issueId')
  AND (COALESCE(NULLIF(t.source_ref->>'issueId',''),plan.issue_id) IS NULL OR EXISTS (
      SELECT 1 FROM repomesh_issues.issue_repository_scope scope
      WHERE scope.project_id=p.id AND scope.repository_id=matched.id
        AND scope.issue_id=COALESCE(NULLIF(t.source_ref->>'issueId',''),plan.issue_id)));

CREATE VIEW public.task_repository_scope_violations AS
SELECT t.id AS task_id, t.project_id, t.repository_id, t.source_ref,
    CASE WHEN scope.task_id IS NULL THEN 'repository_scope_unconfirmed' ELSE 'issue_scope_unconfirmed' END AS reason
FROM public.tasks t LEFT JOIN public.task_repository_scopes scope ON scope.task_id=t.id
WHERE NULLIF(t.repository_id,'') IS NOT NULL AND (scope.task_id IS NULL OR scope.issue_id IS NULL);

CREATE FUNCTION public.check_plan_issue_scope() RETURNS trigger AS $$
BEGIN
    IF TG_OP='UPDATE' AND OLD.issue_id IS NOT NULL
       AND (NEW.issue_id IS DISTINCT FROM OLD.issue_id OR NEW.project_id<>OLD.project_id) THEN
        RAISE EXCEPTION 'plan issue identity is immutable' USING ERRCODE='check_violation';
    END IF;
    IF TG_OP='UPDATE' AND (NEW.issue_id IS DISTINCT FROM OLD.issue_id OR NEW.project_id<>OLD.project_id)
       AND EXISTS (SELECT 1 FROM public.tasks WHERE plan_id=OLD.id) THEN
        RAISE EXCEPTION 'cannot change the scope of a plan with existing tasks' USING ERRCODE='check_violation';
    END IF;
    IF NEW.issue_id IS NOT NULL AND NOT EXISTS (
        SELECT 1 FROM repomesh_issues.issues i
        WHERE i.id=NEW.issue_id AND i.project_id=NEW.project_id::text) THEN
        RAISE EXCEPTION 'plan issue outside project' USING ERRCODE='foreign_key_violation';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER plan_issue_scope BEFORE INSERT OR UPDATE OF project_id, issue_id ON public.plans
    FOR EACH ROW EXECUTE FUNCTION public.check_plan_issue_scope();

CREATE FUNCTION public.bind_task_repository_scope() RETURNS trigger AS $$
DECLARE
    resolved_repo text;
    match_count integer;
    selected_issue text;
    plan_issue text;
    previous_issue text;
BEGIN
    IF NOT EXISTS (SELECT 1 FROM repomesh_projects.projects p
                   WHERE p.id=NEW.project_id::text AND p.organization_id=NEW.organization_id) THEN
        RAISE EXCEPTION 'task project outside organization' USING ERRCODE='foreign_key_violation';
    END IF;
    IF NEW.plan_id IS NOT NULL THEN
        SELECT issue_id INTO plan_issue FROM public.plans
        WHERE id=NEW.plan_id AND project_id=NEW.project_id FOR KEY SHARE;
        IF NOT FOUND THEN
            RAISE EXCEPTION 'task plan outside project' USING ERRCODE='foreign_key_violation';
        END IF;
    END IF;
    selected_issue := COALESCE(NULLIF(NEW.source_ref->>'issueId',''), plan_issue);
    IF plan_issue IS NOT NULL AND selected_issue<>plan_issue THEN
        RAISE EXCEPTION 'task issue differs from plan' USING ERRCODE='check_violation';
    END IF;
    SELECT issue_id INTO previous_issue FROM public.task_repository_scopes WHERE task_id=NEW.id;
    IF previous_issue IS NOT NULL AND selected_issue IS DISTINCT FROM previous_issue THEN
        RAISE EXCEPTION 'task issue identity is immutable' USING ERRCODE='check_violation';
    END IF;
    IF NULLIF(NEW.repository_id,'') IS NULL THEN
        IF selected_issue IS NOT NULL THEN
            RAISE EXCEPTION 'issue task requires repository' USING ERRCODE='check_violation';
        END IF;
        DELETE FROM public.task_repository_scopes WHERE task_id=NEW.id;
        RETURN NEW;
    END IF;
    SELECT min(r.id), count(*) INTO resolved_repo, match_count
    FROM repomesh_projects.project_repositories pr
    JOIN repomesh_projects.repositories r ON r.id=pr.repository_id
    WHERE pr.project_id=NEW.project_id::text
      AND NEW.repository_id IN (r.id, r.owner || '/' || r.name, r.name);
    IF match_count<>1 THEN
        RAISE EXCEPTION 'task repository outside project or ambiguous' USING ERRCODE='foreign_key_violation';
    END IF;
    INSERT INTO public.task_repository_scopes(task_id, project_id, repository_id, issue_id)
    VALUES (NEW.id,NEW.project_id::text,resolved_repo,selected_issue)
    ON CONFLICT(task_id) DO UPDATE SET project_id=EXCLUDED.project_id,
        repository_id=EXCLUDED.repository_id,issue_id=EXCLUDED.issue_id;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER task_repository_scope AFTER INSERT OR UPDATE OF
    project_id, organization_id, plan_id, repository_id, source_ref ON public.tasks
    FOR EACH ROW EXECUTE FUNCTION public.bind_task_repository_scope();

COMMENT ON VIEW public.task_repository_scope_violations IS
    'Historical tasks without a valid project/issue repository binding; never dispatch until explicitly repaired.';

-- The binding is a projection of its task, not an independent writable grant.
CREATE FUNCTION public.check_task_scope_binding() RETURNS trigger AS $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM public.tasks t
        JOIN repomesh_projects.projects p ON p.id=t.project_id::text AND p.organization_id=t.organization_id
        JOIN repomesh_projects.repositories r ON r.id=NEW.repository_id
        LEFT JOIN public.plans plan ON plan.id=t.plan_id AND plan.project_id=t.project_id
        WHERE t.id=NEW.task_id AND t.project_id::text=NEW.project_id
          AND t.repository_id IN (r.id,r.owner || '/' || r.name,r.name)
          AND (t.plan_id IS NULL OR plan.id IS NOT NULL)
          AND NEW.issue_id IS NOT DISTINCT FROM COALESCE(NULLIF(t.source_ref->>'issueId',''),plan.issue_id)
    ) THEN
        RAISE EXCEPTION 'scope binding differs from task identity' USING ERRCODE='check_violation';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER task_scope_binding BEFORE INSERT OR UPDATE ON public.task_repository_scopes
    FOR EACH ROW EXECUTE FUNCTION public.check_task_scope_binding();
