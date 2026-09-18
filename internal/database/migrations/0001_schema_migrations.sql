CREATE TABLE public.repomesh_schema_migrations (
    version bigint PRIMARY KEY CHECK (version > 0),
    name text NOT NULL UNIQUE,
    checksum bytea NOT NULL CHECK (octet_length(checksum) = 32),
    applied_at timestamptz NOT NULL DEFAULT now()
);
