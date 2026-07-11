-- platform:migrate:non-transactional
SET lock_timeout = '5s';
SET statement_timeout = '30s';

CREATE TABLE IF NOT EXISTS job_roles (
    id uuid PRIMARY KEY,
    tenant_id text NOT NULL DEFAULT 'default',
    name text NOT NULL,
    slug text NOT NULL,
    mission text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    archived_at timestamptz NULL,
    trashed_at timestamptz NULL,
    purge_after timestamptz NULL,
    CONSTRAINT job_roles_tenant_slug_unique UNIQUE (tenant_id, slug)
);

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_job_roles_lifecycle
    ON job_roles (tenant_id, archived_at, trashed_at);

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_job_roles_tenant_id
    ON job_roles (tenant_id, id);

CREATE TABLE IF NOT EXISTS profile_templates (
    id uuid PRIMARY KEY,
    tenant_id text NOT NULL DEFAULT 'default',
    name text NOT NULL,
    description text NOT NULL DEFAULT '',
    system_prompt text NOT NULL,
    max_autonomy text NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    archived_at timestamptz NULL,
    trashed_at timestamptz NULL,
    purge_after timestamptz NULL,
    CONSTRAINT profile_templates_max_autonomy_check CHECK (
        max_autonomy IN ('A0', 'A1', 'A2', 'A3', 'A4', 'A5')
    )
);

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_profile_templates_lifecycle
    ON profile_templates (tenant_id, archived_at, trashed_at);

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_profile_templates_tenant_id
    ON profile_templates (tenant_id, id);

CREATE TABLE IF NOT EXISTS virployees (
    id uuid PRIMARY KEY,
    tenant_id text NOT NULL DEFAULT 'default',
    name text NOT NULL,
    job_role_id uuid NOT NULL REFERENCES job_roles(id),
    profile_template_id uuid NOT NULL REFERENCES profile_templates(id),
    description text NOT NULL DEFAULT '',
    supervisor_user_id text NOT NULL,
    autonomy text NOT NULL DEFAULT 'A1',
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    archived_at timestamptz NULL,
    trashed_at timestamptz NULL,
    purge_after timestamptz NULL,
    CONSTRAINT virployees_autonomy_check CHECK (autonomy IN ('A0', 'A1', 'A2', 'A3', 'A4', 'A5'))
);

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_virployees_lifecycle
    ON virployees (tenant_id, archived_at, trashed_at);

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_virployees_tenant_id
    ON virployees (tenant_id, id);

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_virployees_profile_template_id
    ON virployees (tenant_id, profile_template_id);

CREATE TABLE IF NOT EXISTS capabilities (
    id uuid PRIMARY KEY,
    tenant_id text NOT NULL DEFAULT 'default',
    capability_key text NOT NULL,
    name text NOT NULL,
    description text NOT NULL DEFAULT '',
    required_autonomy text NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    archived_at timestamptz NULL,
    trashed_at timestamptz NULL,
    purge_after timestamptz NULL,
    CONSTRAINT capabilities_tenant_key_unique UNIQUE (tenant_id, capability_key),
    CONSTRAINT capabilities_required_autonomy_check CHECK (
        required_autonomy IN ('A0', 'A1', 'A2', 'A3', 'A4', 'A5')
    ),
    CONSTRAINT capabilities_key_format_check CHECK (
        capability_key ~ '^[a-zñ]+\.[a-zñ]+\.[a-zñ]+$'
    )
);

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_capabilities_lifecycle
    ON capabilities (tenant_id, archived_at, trashed_at);

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_capabilities_tenant_id
    ON capabilities (tenant_id, id);

CREATE TABLE IF NOT EXISTS virployee_capabilities (
    tenant_id text NOT NULL DEFAULT 'default',
    virployee_id uuid NOT NULL REFERENCES virployees(id) ON DELETE CASCADE,
    capability_id uuid NOT NULL REFERENCES capabilities(id),
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, virployee_id, capability_id)
);

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_virployee_capabilities_capability_id
    ON virployee_capabilities (tenant_id, capability_id);
