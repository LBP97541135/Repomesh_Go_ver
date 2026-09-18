-- Human-control surface (ReviewDesk): checkpoint decisions plus the admin
-- flag that scopes who sees the whole queue versus only their own items.
ALTER TABLE repomesh_access.accounts ADD COLUMN IF NOT EXISTS is_admin boolean NOT NULL DEFAULT false;

CREATE TABLE IF NOT EXISTS public.checkpoint_decisions (
    id uuid PRIMARY KEY,
    review_request_id uuid NOT NULL REFERENCES public.review_requests(id),
    project_id uuid NOT NULL,
    checkpoint text NOT NULL,
    human_principal_id text NOT NULL,
    decision text NOT NULL CHECK (decision IN ('approved', 'rejected', 'changes_requested')),
    reason text NOT NULL DEFAULT '',
    repository_id text,
    evidence_version text NOT NULL DEFAULT '',
    decided_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX IF NOT EXISTS uq_checkpoint_decisions_request
    ON public.checkpoint_decisions (review_request_id);
CREATE INDEX IF NOT EXISTS idx_checkpoint_decisions_project
    ON public.checkpoint_decisions (project_id);

ALTER TABLE public.review_requests ADD COLUMN IF NOT EXISTS checkpoint text NOT NULL DEFAULT 'execution';
ALTER TABLE public.review_requests ADD COLUMN IF NOT EXISTS evidence_version text NOT NULL DEFAULT '';
ALTER TABLE public.review_requests ADD COLUMN IF NOT EXISTS title text NOT NULL DEFAULT '';
ALTER TABLE public.review_requests ADD COLUMN IF NOT EXISTS summary text NOT NULL DEFAULT '';
ALTER TABLE public.review_requests ADD COLUMN IF NOT EXISTS repository_id text;
ALTER TABLE public.review_requests ADD COLUMN IF NOT EXISTS assignee text;
ALTER TABLE public.review_requests ADD COLUMN IF NOT EXISTS updated_at timestamptz NOT NULL DEFAULT now();

CREATE INDEX IF NOT EXISTS idx_review_requests_assignee
    ON public.review_requests (assignee) WHERE assignee IS NOT NULL;
