SET lock_timeout = '5s';
SET statement_timeout = '30s';

-- Additive replacement for the historically named Nexus outbox. The old
-- tables remain untouched for audit/rollback during the compatibility window.
CREATE TABLE IF NOT EXISTS companion_outbox_messages
    (LIKE companion_nexus_outbox INCLUDING ALL);

INSERT INTO companion_outbox_messages
SELECT *
FROM companion_nexus_outbox
ON CONFLICT (id) DO NOTHING;

CREATE TABLE IF NOT EXISTS companion_outbox_events (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    outbox_id uuid NOT NULL REFERENCES companion_outbox_messages(id) ON DELETE CASCADE,
    event text NOT NULL CHECK (btrim(event) <> ''),
    metadata_json jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_companion_outbox_events_message
    ON companion_outbox_events (outbox_id, created_at, id);

INSERT INTO companion_outbox_events (id, outbox_id, event, metadata_json, created_at)
SELECT event.id, event.outbox_id, event.event, event.metadata_json, event.created_at
FROM companion_nexus_outbox_events AS event
JOIN companion_outbox_messages AS message ON message.id = event.outbox_id
ON CONFLICT (id) DO NOTHING;

DROP VIEW IF EXISTS companion_outbox;
CREATE VIEW companion_outbox AS
SELECT id, org_id, destination, contract_version, aggregate_type, aggregate_id,
       kind, dedupe_key, payload_json, status, attempts, max_attempts,
       available_at, lease_owner, lease_until, heartbeat_at, last_error_code,
       created_at, updated_at, delivered_at
FROM companion_outbox_messages;

COMMENT ON TABLE companion_outbox_messages IS
    'Transport-neutral, versioned transactional outbox. Destination adapters translate payloads at delivery time.';
COMMENT ON COLUMN companion_outbox_events.metadata_json IS
    'Delivery metadata only: never execution payloads, PHI, secrets, signed URLs, or raw errors.';
