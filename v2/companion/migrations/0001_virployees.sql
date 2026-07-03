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

CREATE INDEX IF NOT EXISTS idx_job_roles_lifecycle
    ON job_roles (tenant_id, archived_at, trashed_at);

CREATE INDEX IF NOT EXISTS idx_job_roles_tenant_id
    ON job_roles (tenant_id, id);

CREATE TABLE IF NOT EXISTS virployees (
    id uuid PRIMARY KEY,
    tenant_id text NOT NULL DEFAULT 'default',
    name text NOT NULL,
    job_role_id uuid NOT NULL REFERENCES job_roles(id),
    description text NOT NULL DEFAULT '',
    supervisor_user_id uuid NOT NULL,
    autonomy text NOT NULL DEFAULT 'A1',
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    archived_at timestamptz NULL,
    trashed_at timestamptz NULL,
    purge_after timestamptz NULL,
    CONSTRAINT virployees_autonomy_check CHECK (autonomy IN ('A0', 'A1', 'A2', 'A3', 'A4', 'A5'))
);

CREATE INDEX IF NOT EXISTS idx_virployees_lifecycle
    ON virployees (tenant_id, archived_at, trashed_at);

CREATE INDEX IF NOT EXISTS idx_virployees_tenant_id
    ON virployees (tenant_id, id);
