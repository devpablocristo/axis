-- platform:migrate:non-transactional
SET lock_timeout = '5s';
SET statement_timeout = '30s';

CREATE TABLE IF NOT EXISTS action_types (
    id uuid PRIMARY KEY,
    tenant_id text NOT NULL DEFAULT 'default',
    action_type_key text NOT NULL,
    name text NOT NULL,
    description text NOT NULL DEFAULT '',
    category text NOT NULL DEFAULT '',
    risk_class text NOT NULL DEFAULT 'low',
    enabled boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT action_types_tenant_key_unique UNIQUE (tenant_id, action_type_key),
    CONSTRAINT action_types_risk_class_check CHECK (
        risk_class IN ('low', 'medium', 'high')
    ),
    CONSTRAINT action_types_key_format_check CHECK (
        action_type_key ~ '^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)+$'
    )
);

ALTER TABLE action_types
    ADD COLUMN IF NOT EXISTS tenant_id text NOT NULL DEFAULT 'default';

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_action_types_tenant_id
    ON action_types (tenant_id, id);

CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_action_types_tenant_key_unique
    ON action_types (tenant_id, action_type_key);

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_action_types_tenant_key
    ON action_types (tenant_id, action_type_key);

INSERT INTO action_types (
    id, tenant_id, action_type_key, name, description, category, risk_class, enabled
) VALUES
    ('00000000-0000-0000-0000-000000000101', 'default', 'calendar.events.read', 'Read calendar events', 'Read events from a tenant calendar.', 'calendar', 'low', true),
    ('00000000-0000-0000-0000-000000000102', 'default', 'calendar.events.create', 'Create calendar events', 'Prepare or create events in a tenant calendar.', 'calendar', 'medium', true),
    ('00000000-0000-0000-0000-000000000103', 'default', 'calendar.events.update', 'Update calendar events', 'Prepare or update events in a tenant calendar.', 'calendar', 'medium', true),
    ('00000000-0000-0000-0000-000000000104', 'default', 'calendar.events.delete', 'Delete calendar events', 'Delete events from a tenant calendar.', 'calendar', 'high', true)
ON CONFLICT (tenant_id, action_type_key) DO NOTHING;
