CREATE TABLE IF NOT EXISTS capabilities (
    id uuid PRIMARY KEY,
    tenant_id text NOT NULL DEFAULT 'default',
    capability_key text NOT NULL,
    name text NOT NULL,
    description text NOT NULL DEFAULT '',
    required_autonomy text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
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

CREATE INDEX IF NOT EXISTS idx_capabilities_lifecycle
    ON capabilities (tenant_id, archived_at, trashed_at);

CREATE INDEX IF NOT EXISTS idx_capabilities_tenant_id
    ON capabilities (tenant_id, id);

CREATE TABLE IF NOT EXISTS virployee_capabilities (
    tenant_id text NOT NULL DEFAULT 'default',
    virployee_id uuid NOT NULL REFERENCES virployees(id) ON DELETE CASCADE,
    capability_id uuid NOT NULL REFERENCES capabilities(id),
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, virployee_id, capability_id)
);

CREATE INDEX IF NOT EXISTS idx_virployee_capabilities_capability_id
    ON virployee_capabilities (tenant_id, capability_id);
