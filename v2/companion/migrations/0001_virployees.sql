CREATE TABLE IF NOT EXISTS virployees (
    id uuid PRIMARY KEY,
    tenant_id text NOT NULL DEFAULT 'default',
    name text NOT NULL,
    role text NOT NULL,
    description text NOT NULL DEFAULT '',
    supervisor_user_id uuid NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    archived_at timestamptz NULL,
    trashed_at timestamptz NULL,
    purge_after timestamptz NULL
);

CREATE INDEX IF NOT EXISTS idx_virployees_lifecycle
    ON virployees (tenant_id, archived_at, trashed_at);

CREATE INDEX IF NOT EXISTS idx_virployees_tenant_id
    ON virployees (tenant_id, id);
