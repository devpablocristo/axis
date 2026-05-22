-- Plan 3: result idempotency and tenant-aware action types.

ALTER TABLE action_types
    ADD COLUMN IF NOT EXISTS org_id TEXT;

ALTER TABLE action_types
    DROP CONSTRAINT IF EXISTS action_types_name_key;

DROP INDEX IF EXISTS action_types_name_key;
DROP INDEX IF EXISTS idx_action_types_name;

CREATE UNIQUE INDEX IF NOT EXISTS idx_action_types_global_name
    ON action_types (name)
    WHERE org_id IS NULL;

CREATE UNIQUE INDEX IF NOT EXISTS idx_action_types_org_name
    ON action_types (org_id, name)
    WHERE org_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_action_types_org_enabled
    ON action_types (org_id, enabled, category, name);

CREATE TABLE IF NOT EXISTS request_result_reports (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    request_id    UUID NOT NULL REFERENCES requests(id) ON DELETE CASCADE,
    result_key    TEXT NOT NULL,
    actor_id      TEXT NOT NULL DEFAULT '',
    org_id        TEXT,
    success       BOOLEAN NOT NULL,
    result_json   JSONB NOT NULL DEFAULT '{}',
    error_message TEXT NOT NULL DEFAULT '',
    duration_ms   BIGINT NOT NULL DEFAULT 0,
    payload_hash  TEXT NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_request_result_reports_key
    ON request_result_reports (request_id, result_key);

CREATE INDEX IF NOT EXISTS idx_request_result_reports_org_created
    ON request_result_reports (org_id, created_at DESC)
    WHERE org_id IS NOT NULL;
