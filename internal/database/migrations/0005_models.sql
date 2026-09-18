CREATE SCHEMA repomesh_sources;
CREATE SCHEMA repomesh_models;

ALTER TABLE repomesh_projects.defaults
    ADD COLUMN pinned_version text CHECK (pinned_version IS NULL OR length(pinned_version) BETWEEN 1 AND 128);

ALTER TABLE repomesh_projects.defaults
    ADD CONSTRAINT defaults_pinned_version_fk
    FOREIGN KEY (kind, profile_id, pinned_version)
    REFERENCES repomesh_projects.profile_versions(kind, profile_id, version);

ALTER TABLE repomesh_projects.cursors
    DROP CONSTRAINT cursors_kind_check;
ALTER TABLE repomesh_projects.cursors
    ADD CONSTRAINT cursors_kind_check
    CHECK (kind IN ('projects', 'repositories', 'model', 'execution', 'providers'));

CREATE TABLE repomesh_sources.deployment (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    deployment_id text NOT NULL UNIQUE CHECK (deployment_id ~ '^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$')
);
INSERT INTO repomesh_sources.deployment(singleton, deployment_id) VALUES (true, '00000000-0000-4000-8000-000000000001');

CREATE TABLE repomesh_sources.import_serialization (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton)
);
INSERT INTO repomesh_sources.import_serialization(singleton) VALUES (true);

CREATE TABLE repomesh_sources.imports (
    deployment_id text NOT NULL REFERENCES repomesh_sources.deployment(deployment_id),
    import_id text NOT NULL CHECK (import_id ~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'),
    schema_version integer NOT NULL CHECK (schema_version = 1),
    canonical bytea NOT NULL,
    receipt jsonb NOT NULL,
    committed_at timestamptz NOT NULL,
    PRIMARY KEY (deployment_id, import_id)
);

CREATE TABLE repomesh_sources.environment_templates (
    id text NOT NULL CHECK (length(id) BETWEEN 1 AND 128),
    version text NOT NULL CHECK (length(version) BETWEEN 1 AND 128),
    executor_pool_id text NOT NULL CHECK (length(executor_pool_id) BETWEEN 1 AND 128),
    approved_template_digest text NOT NULL CHECK (approved_template_digest ~ '^sha256:[0-9a-f]{64}$'),
    network_policy_ref text NOT NULL CHECK (length(network_policy_ref) BETWEEN 1 AND 128),
    resource_class_id text NOT NULL CHECK (length(resource_class_id) BETWEEN 1 AND 128),
    enabled boolean NOT NULL,
    PRIMARY KEY (id, version)
);

CREATE TABLE repomesh_sources.execution_versions (
    profile_id text NOT NULL,
    version text NOT NULL CHECK (length(version) BETWEEN 1 AND 128),
    owner text NOT NULL REFERENCES repomesh_access.accounts(id),
    name text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 200),
    template_id text NOT NULL,
    template_version text NOT NULL,
    worker_concurrency integer NOT NULL CHECK (worker_concurrency BETWEEN 1 AND 16),
    verification_group_enabled boolean NOT NULL,
    profile_kind text GENERATED ALWAYS AS ('execution') STORED,
    PRIMARY KEY (profile_id, version),
    FOREIGN KEY (template_id, template_version)
        REFERENCES repomesh_sources.environment_templates(id, version),
    FOREIGN KEY (profile_kind, profile_id, owner)
        REFERENCES repomesh_projects.profiles(kind, id, owner),
    FOREIGN KEY (profile_kind, profile_id, version)
        REFERENCES repomesh_projects.profile_versions(kind, profile_id, version)
);

CREATE TABLE repomesh_models.providers (
    id text PRIMARY KEY CHECK (id ~ '^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'),
    owner text NOT NULL REFERENCES repomesh_access.accounts(id),
    head_revision text NOT NULL CHECK (head_revision ~ '^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'),
    enabled boolean NOT NULL DEFAULT true,
    access_epoch bigint NOT NULL DEFAULT 1,
    UNIQUE (id, owner)
);

CREATE TABLE repomesh_models.provider_revisions (
    provider_id text NOT NULL,
    revision text NOT NULL CHECK (revision ~ '^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'),
    owner text NOT NULL REFERENCES repomesh_access.accounts(id),
    name text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 100),
    base_url text NOT NULL CHECK (char_length(base_url) BETWEEN 1 AND 2048),
    api_format text NOT NULL CHECK (api_format = 'openai_chat_completions'),
    secret_version_id text NOT NULL,
    secret_owner_kind text NOT NULL CHECK (secret_owner_kind = 'model-provider'),
    secret_owner_id text NOT NULL,
    secret_purpose text NOT NULL CHECK (secret_purpose = 'model-provider-key'),
    created_at timestamptz NOT NULL,
    PRIMARY KEY (provider_id, revision),
    FOREIGN KEY (provider_id, owner) REFERENCES repomesh_models.providers(id, owner),
    FOREIGN KEY (secret_version_id, secret_owner_kind, secret_owner_id, secret_purpose)
        REFERENCES repomesh_secrets.versions(version_id, owner_kind, owner_id, purpose),
    CHECK (secret_owner_id = provider_id)
);

ALTER TABLE repomesh_models.providers
    ADD CONSTRAINT provider_head_revision_fk
    FOREIGN KEY (id, head_revision)
    REFERENCES repomesh_models.provider_revisions(provider_id, revision)
    DEFERRABLE INITIALLY DEFERRED;

CREATE TABLE repomesh_models.model_rows (
    id text PRIMARY KEY CHECK (id ~ '^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'),
    provider_id text NOT NULL,
    owner text NOT NULL REFERENCES repomesh_access.accounts(id),
    profile_id text NOT NULL UNIQUE CHECK (length(profile_id) BETWEEN 1 AND 128),
    profile_kind text GENERATED ALWAYS AS ('model') STORED,
    UNIQUE (provider_id, id),
    FOREIGN KEY (provider_id, owner) REFERENCES repomesh_models.providers(id, owner),
    FOREIGN KEY (profile_kind, profile_id, owner) REFERENCES repomesh_projects.profiles(kind, id, owner)
);

CREATE TABLE repomesh_models.model_snapshots (
    provider_id text NOT NULL,
    provider_revision text NOT NULL,
    row_id text NOT NULL,
    model_id text NOT NULL CHECK (char_length(model_id) BETWEEN 1 AND 200),
    display_name text NOT NULL CHECK (char_length(display_name) BETWEEN 1 AND 200),
    context_window bigint NOT NULL CHECK (context_window BETWEEN 1 AND 2147483647),
    max_output_tokens bigint NOT NULL CHECK (max_output_tokens BETWEEN 1 AND 2147483647 AND max_output_tokens <= context_window),
    reasoning boolean NOT NULL,
    vision boolean NOT NULL,
    PRIMARY KEY (provider_id, provider_revision, row_id),
    UNIQUE (provider_id, provider_revision, model_id),
    FOREIGN KEY (provider_id, provider_revision) REFERENCES repomesh_models.provider_revisions(provider_id, revision),
    FOREIGN KEY (provider_id, row_id) REFERENCES repomesh_models.model_rows(provider_id, id)
);

CREATE TABLE repomesh_models.profile_links (
    profile_id text NOT NULL,
    profile_version text NOT NULL,
    provider_id text NOT NULL,
    provider_revision text NOT NULL,
    row_id text NOT NULL,
    owner text NOT NULL,
    secret_version_id text NOT NULL,
    profile_kind text GENERATED ALWAYS AS ('model') STORED,
    PRIMARY KEY (profile_id, profile_version),
    CHECK (profile_version = provider_revision),
    FOREIGN KEY (profile_kind, profile_id, profile_version)
        REFERENCES repomesh_projects.profile_versions(kind, profile_id, version),
    FOREIGN KEY (provider_id, provider_revision, row_id)
        REFERENCES repomesh_models.model_snapshots(provider_id, provider_revision, row_id)
);

CREATE TABLE repomesh_models.save_operations (
    actor text NOT NULL REFERENCES repomesh_access.accounts(id),
    save_id text NOT NULL CHECK (save_id ~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'),
    kind text NOT NULL CHECK (kind IN ('save_input', 'save_closed')),
    outcome text NOT NULL CHECK (outcome IN ('committed', 'rejected', 'closed_without_save')),
    target text,
    schema_version integer,
    canonical_nonsecret bytea,
    input_vault_version text,
    receipt jsonb,
    removed_at timestamptz,
    PRIMARY KEY (actor, save_id),
    FOREIGN KEY (target, actor) REFERENCES repomesh_models.providers(id, owner)
);

CREATE FUNCTION repomesh_models.reject_immutable_change() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'immutable RepoMesh model fact';
END;
$$;

CREATE TRIGGER provider_revisions_immutable
BEFORE UPDATE OR DELETE ON repomesh_models.provider_revisions
FOR EACH ROW EXECUTE FUNCTION repomesh_models.reject_immutable_change();

CREATE TRIGGER model_snapshots_immutable
BEFORE UPDATE OR DELETE ON repomesh_models.model_snapshots
FOR EACH ROW EXECUTE FUNCTION repomesh_models.reject_immutable_change();

CREATE TRIGGER environment_templates_immutable
BEFORE UPDATE OR DELETE ON repomesh_sources.environment_templates
FOR EACH ROW EXECUTE FUNCTION repomesh_models.reject_immutable_change();

CREATE TRIGGER execution_versions_immutable
BEFORE UPDATE OR DELETE ON repomesh_sources.execution_versions
FOR EACH ROW EXECUTE FUNCTION repomesh_models.reject_immutable_change();

CREATE FUNCTION repomesh_models.require_complete_save() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE complete boolean;
BEGIN
    SELECT CASE
        WHEN removed_at IS NOT NULL THEN
            target IS NOT NULL AND receipt IS NULL AND canonical_nonsecret IS NULL AND input_vault_version IS NULL
        WHEN kind = 'save_closed' THEN
            outcome = 'closed_without_save' AND receipt IS NOT NULL AND target IS NULL
            AND canonical_nonsecret IS NULL AND input_vault_version IS NULL AND schema_version IS NULL
        WHEN kind = 'save_input' AND outcome IN ('committed', 'rejected') THEN
            schema_version IS NOT NULL AND canonical_nonsecret IS NOT NULL AND receipt IS NOT NULL
            AND (outcome <> 'committed' OR target IS NOT NULL)
            AND (outcome <> 'committed' OR input_vault_version IS NOT NULL OR true)
        ELSE false
    END
    INTO complete
    FROM repomesh_models.save_operations WHERE actor = NEW.actor AND save_id = NEW.save_id;
    IF complete IS DISTINCT FROM true THEN
        RAISE EXCEPTION 'incomplete RepoMesh model save';
    END IF;
    RETURN NULL;
END;
$$;

CREATE CONSTRAINT TRIGGER save_operation_complete
AFTER INSERT OR UPDATE ON repomesh_models.save_operations
DEFERRABLE INITIALLY DEFERRED FOR EACH ROW
EXECUTE FUNCTION repomesh_models.require_complete_save();

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'repomesh_source_importer') THEN
        CREATE ROLE repomesh_source_importer NOLOGIN;
    END IF;
END
$$;
GRANT repomesh_source_importer TO CURRENT_USER;
