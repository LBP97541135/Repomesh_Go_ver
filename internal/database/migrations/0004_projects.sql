CREATE SCHEMA repomesh_projects;

ALTER TABLE repomesh_secrets.versions
    ADD CONSTRAINT secret_version_owner_purpose_unique
    UNIQUE (version_id, owner_kind, owner_id, purpose);

CREATE TABLE repomesh_projects.catalog (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton)
);
INSERT INTO repomesh_projects.catalog(singleton) VALUES (true);

CREATE TABLE repomesh_projects.profiles (
    kind text NOT NULL CHECK (kind IN ('model', 'execution')),
    id text NOT NULL CHECK (length(id) BETWEEN 1 AND 128),
    owner text NOT NULL REFERENCES repomesh_access.accounts(id),
    name text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 200),
    enabled boolean NOT NULL DEFAULT true,
    current_version text NOT NULL CHECK (length(current_version) BETWEEN 1 AND 128),
    PRIMARY KEY (kind, id),
    UNIQUE (kind, id, owner)
);

CREATE TABLE repomesh_projects.profile_versions (
    kind text NOT NULL,
    profile_id text NOT NULL,
    version text NOT NULL CHECK (length(version) BETWEEN 1 AND 128),
    secret_version_id text,
    secret_owner_kind text,
    secret_owner_id text,
    secret_purpose text,
    parameters_complete boolean NOT NULL DEFAULT false,
    worker_concurrency integer CHECK (worker_concurrency BETWEEN 1 AND 16),
    budget_policy_id text,
    time_limit_policy_id text,
    verification_group_enabled boolean,
    PRIMARY KEY (kind, profile_id, version),
    FOREIGN KEY (kind, profile_id) REFERENCES repomesh_projects.profiles(kind, id),
    FOREIGN KEY (secret_version_id, secret_owner_kind, secret_owner_id, secret_purpose)
        REFERENCES repomesh_secrets.versions(version_id, owner_kind, owner_id, purpose),
    CHECK ((secret_version_id IS NULL AND secret_owner_kind IS NULL AND secret_owner_id IS NULL AND secret_purpose IS NULL)
        OR (secret_version_id IS NOT NULL AND secret_owner_kind IS NOT NULL AND secret_owner_id IS NOT NULL AND secret_purpose IS NOT NULL))
);

ALTER TABLE repomesh_projects.profiles
    ADD CONSTRAINT profile_current_version_fk
    FOREIGN KEY (kind, id, current_version)
    REFERENCES repomesh_projects.profile_versions(kind, profile_id, version)
    DEFERRABLE INITIALLY DEFERRED;

CREATE TABLE repomesh_projects.defaults (
    actor text NOT NULL REFERENCES repomesh_access.accounts(id),
    kind text NOT NULL CHECK (kind IN ('model', 'execution')),
    profile_id text NOT NULL,
    default_revision text NOT NULL CHECK (default_revision ~ '^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'),
    PRIMARY KEY (actor, kind),
    FOREIGN KEY (kind, profile_id, actor) REFERENCES repomesh_projects.profiles(kind, id, owner)
);

CREATE TABLE repomesh_projects.projects (
    id text PRIMARY KEY CHECK (id ~ '^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'),
    owner text NOT NULL REFERENCES repomesh_access.accounts(id),
    name text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 200),
    purpose text NOT NULL CHECK (char_length(purpose) BETWEEN 1 AND 20000),
    revision text NOT NULL CHECK (revision ~ '^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'),
    creation_context_revision text NOT NULL CHECK (creation_context_revision ~ '^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'),
    current_configuration_revision text NOT NULL CHECK (current_configuration_revision ~ '^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    removed_at timestamptz,
    UNIQUE (id, current_configuration_revision)
);
CREATE INDEX projects_owner_id ON repomesh_projects.projects(owner, id);

CREATE TABLE repomesh_projects.repositories (
    id text PRIMARY KEY CHECK (id ~ '^repo_[0-9]{20}$'),
    host text NOT NULL CHECK (host = 'github.com'),
    github_id bigint NOT NULL CHECK (github_id > 0),
    owner text NOT NULL,
    name text NOT NULL,
    UNIQUE (host, github_id)
);

CREATE TABLE repomesh_projects.project_repositories (
    project_id text NOT NULL REFERENCES repomesh_projects.projects(id),
    repository_id text NOT NULL REFERENCES repomesh_projects.repositories(id),
    joined_revision text NOT NULL,
    joined_at timestamptz NOT NULL,
    PRIMARY KEY (project_id, repository_id)
);

CREATE TABLE repomesh_projects.configuration_revisions (
    project_id text NOT NULL REFERENCES repomesh_projects.projects(id),
    revision text NOT NULL,
    fixed jsonb NOT NULL CHECK (jsonb_typeof(fixed) = 'object'),
    model_kind text NOT NULL DEFAULT 'model' CHECK (model_kind = 'model'),
    model_profile_id text,
    model_profile_version text,
    execution_kind text NOT NULL DEFAULT 'execution' CHECK (execution_kind = 'execution'),
    execution_profile_id text,
    execution_profile_version text,
    created_by text NOT NULL REFERENCES repomesh_access.accounts(id),
    created_at timestamptz NOT NULL,
    PRIMARY KEY (project_id, revision),
    FOREIGN KEY (model_kind, model_profile_id, model_profile_version)
        REFERENCES repomesh_projects.profile_versions(kind, profile_id, version),
    FOREIGN KEY (execution_kind, execution_profile_id, execution_profile_version)
        REFERENCES repomesh_projects.profile_versions(kind, profile_id, version),
    CHECK ((model_profile_id IS NULL) = (model_profile_version IS NULL)),
    CHECK ((execution_profile_id IS NULL) = (execution_profile_version IS NULL)),
    CHECK ((fixed->'model'->>'profileId') IS NOT DISTINCT FROM model_profile_id),
    CHECK ((fixed->'model'->>'version') IS NOT DISTINCT FROM model_profile_version),
    CHECK ((fixed->'execution'->>'profileId') IS NOT DISTINCT FROM execution_profile_id),
    CHECK ((fixed->'execution'->>'version') IS NOT DISTINCT FROM execution_profile_version)
);

ALTER TABLE repomesh_projects.projects
    ADD CONSTRAINT project_current_configuration_fk
    FOREIGN KEY (id, current_configuration_revision)
    REFERENCES repomesh_projects.configuration_revisions(project_id, revision)
    DEFERRABLE INITIALLY DEFERRED;

CREATE TABLE repomesh_projects.creation_operations (
    actor text NOT NULL REFERENCES repomesh_access.accounts(id),
    key text NOT NULL CHECK (key ~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'),
    schema_version integer NOT NULL CHECK (schema_version = 1),
    canonical_input bytea,
    exact_input bytea,
    project_id text REFERENCES repomesh_projects.projects(id) DEFERRABLE INITIALLY DEFERRED,
    project_revision text,
    committed_at timestamptz,
    removed_at timestamptz,
    PRIMARY KEY (actor, key),
    CHECK (removed_at IS NULL OR (canonical_input IS NULL AND exact_input IS NULL))
);

CREATE TABLE repomesh_projects.update_operations (
    project_id text NOT NULL REFERENCES repomesh_projects.projects(id),
    actor text NOT NULL REFERENCES repomesh_access.accounts(id),
    key text NOT NULL CHECK (key ~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'),
    schema_version integer NOT NULL CHECK (schema_version = 1),
    canonical_input bytea,
    exact_input bytea,
    project_revision text,
    committed_at timestamptz,
    removed_at timestamptz,
    PRIMARY KEY (project_id, actor, key),
    CHECK (removed_at IS NULL OR (canonical_input IS NULL AND exact_input IS NULL))
);

CREATE TABLE repomesh_projects.cursors (
    id text PRIMARY KEY,
    actor text NOT NULL REFERENCES repomesh_access.accounts(id),
    kind text NOT NULL CHECK (kind IN ('projects', 'repositories', 'model', 'execution')),
    project_id text NOT NULL DEFAULT '',
    query text NOT NULL DEFAULT '',
    page_limit integer NOT NULL CHECK (page_limit BETWEEN 1 AND 100),
    after_id text NOT NULL DEFAULT '',
    project_revision text NOT NULL DEFAULT '',
    expires_at timestamptz NOT NULL
);

CREATE FUNCTION repomesh_projects.reject_immutable_change() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'immutable RepoMesh project version';
END;
$$;

CREATE TRIGGER profile_versions_immutable
BEFORE UPDATE OR DELETE ON repomesh_projects.profile_versions
FOR EACH ROW EXECUTE FUNCTION repomesh_projects.reject_immutable_change();

CREATE TRIGGER configuration_revisions_immutable
BEFORE UPDATE OR DELETE ON repomesh_projects.configuration_revisions
FOR EACH ROW EXECUTE FUNCTION repomesh_projects.reject_immutable_change();

CREATE FUNCTION repomesh_projects.require_complete_operation() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE complete boolean;
BEGIN
	IF TG_TABLE_NAME = 'creation_operations' THEN
		SELECT removed_at IS NOT NULL OR (canonical_input IS NOT NULL AND exact_input IS NOT NULL
			AND project_id IS NOT NULL AND project_revision IS NOT NULL AND committed_at IS NOT NULL)
		INTO complete FROM repomesh_projects.creation_operations WHERE actor=NEW.actor AND key=NEW.key;
	ELSE
		SELECT removed_at IS NOT NULL OR (canonical_input IS NOT NULL AND exact_input IS NOT NULL
			AND project_revision IS NOT NULL AND committed_at IS NOT NULL)
		INTO complete FROM repomesh_projects.update_operations
		WHERE project_id=NEW.project_id AND actor=NEW.actor AND key=NEW.key;
	END IF;
    IF complete IS DISTINCT FROM true THEN
        RAISE EXCEPTION 'incomplete RepoMesh project operation';
    END IF;
    RETURN NULL;
END;
$$;

CREATE CONSTRAINT TRIGGER creation_operation_complete
AFTER INSERT OR UPDATE ON repomesh_projects.creation_operations
DEFERRABLE INITIALLY DEFERRED FOR EACH ROW
EXECUTE FUNCTION repomesh_projects.require_complete_operation();

CREATE CONSTRAINT TRIGGER update_operation_complete
AFTER INSERT OR UPDATE ON repomesh_projects.update_operations
DEFERRABLE INITIALLY DEFERRED FOR EACH ROW
EXECUTE FUNCTION repomesh_projects.require_complete_operation();
