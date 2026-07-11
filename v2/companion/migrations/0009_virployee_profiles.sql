-- platform:migrate:non-transactional
SET lock_timeout = '5s';
SET statement_timeout = '30s';

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

CREATE TABLE IF NOT EXISTS virployee_profiles (
    id uuid PRIMARY KEY,
    tenant_id text NOT NULL DEFAULT 'default',
    virployee_id uuid NOT NULL REFERENCES virployees(id) ON DELETE CASCADE,
    profile_template_id uuid NOT NULL,
    name text NOT NULL,
    description text NOT NULL DEFAULT '',
    system_prompt text NOT NULL,
    max_autonomy text NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CONSTRAINT virployee_profiles_virployee_unique UNIQUE (tenant_id, virployee_id),
    CONSTRAINT virployee_profiles_max_autonomy_check CHECK (
        max_autonomy IN ('A0', 'A1', 'A2', 'A3', 'A4', 'A5')
    )
);

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_virployee_profiles_template_id
    ON virployee_profiles (tenant_id, profile_template_id);
