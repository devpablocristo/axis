SET lock_timeout = '5s';
SET statement_timeout = '30s';

CREATE TABLE IF NOT EXISTS companion_nexus_outbox (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id text NOT NULL CHECK (btrim(tenant_id) <> ''),
    aggregate_type text NOT NULL CHECK (aggregate_type = 'execution_attempt'),
    aggregate_id uuid NOT NULL,
    kind text NOT NULL CHECK (kind = 'execution_result'),
    dedupe_key text NOT NULL CHECK (btrim(dedupe_key) <> ''),
    payload_json jsonb NOT NULL,
    status text NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'processing', 'delivered', 'dead')),
    attempts integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    max_attempts integer NOT NULL DEFAULT 10 CHECK (max_attempts = 10),
    available_at timestamptz NOT NULL DEFAULT now(),
    lease_owner text NOT NULL DEFAULT '',
    lease_until timestamptz,
    heartbeat_at timestamptz,
    last_error_code text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    delivered_at timestamptz,
    UNIQUE (tenant_id, kind, dedupe_key)
);

CREATE INDEX IF NOT EXISTS idx_companion_nexus_outbox_claim
    ON companion_nexus_outbox (available_at, created_at, id)
    WHERE status = 'pending';

CREATE INDEX IF NOT EXISTS idx_companion_nexus_outbox_lease
    ON companion_nexus_outbox (lease_until, id)
    WHERE status = 'processing';

CREATE INDEX IF NOT EXISTS idx_companion_nexus_outbox_aggregate
    ON companion_nexus_outbox (tenant_id, aggregate_type, aggregate_id);

CREATE TABLE IF NOT EXISTS companion_nexus_outbox_events (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    outbox_id uuid NOT NULL REFERENCES companion_nexus_outbox(id) ON DELETE CASCADE,
    event text NOT NULL CHECK (btrim(event) <> ''),
    metadata_json jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_companion_nexus_outbox_events_message
    ON companion_nexus_outbox_events (outbox_id, created_at, id);

COMMENT ON COLUMN companion_nexus_outbox_events.metadata_json IS
    'Delivery metadata only: never execution payloads, PHI, secrets, signed URLs, or raw errors.';
