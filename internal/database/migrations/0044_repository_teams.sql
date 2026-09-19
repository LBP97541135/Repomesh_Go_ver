CREATE TABLE public.repository_teams (
    repository_id text PRIMARY KEY,
    agentteams_team_name text NOT NULL UNIQUE,
    leader_id uuid NOT NULL UNIQUE,
    leader_resource_name text NOT NULL UNIQUE,
    runtime_status text NOT NULL DEFAULT 'Pending',
    roster_revision bigint NOT NULL DEFAULT 1 CHECK (roster_revision > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE public.repository_team_workers (
    id uuid PRIMARY KEY,
    repository_id text NOT NULL REFERENCES public.repository_teams(repository_id),
    resource_name text NOT NULL UNIQUE,
    creation_sequence integer NOT NULL CHECK (creation_sequence > 0),
    display_order integer,
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'disabled')),
    disabled_at timestamptz,
    UNIQUE (repository_id, creation_sequence),
    CHECK ((status = 'active') = (display_order IS NOT NULL))
);

-- The Team root row holds the single leader; this index only orders active Workers.
CREATE UNIQUE INDEX uq_repository_team_active_worker_order
    ON public.repository_team_workers(repository_id, display_order)
    WHERE status = 'active';
