-- Trust boundary Nexus <-> Companion.
-- Adds tenant, actor, idempotency, and evidence metadata to connector-owned data.

ALTER TABLE companion_connectors
    ADD COLUMN IF NOT EXISTS org_id TEXT NOT NULL DEFAULT '';

ALTER TABLE companion_connector_executions
    ADD COLUMN IF NOT EXISTS org_id TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS actor_id TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS idempotency_key TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS evidence_json JSONB NOT NULL DEFAULT '{}';

CREATE INDEX IF NOT EXISTS idx_connectors_org_kind
    ON companion_connectors (org_id, kind);

CREATE INDEX IF NOT EXISTS idx_executions_org_created
    ON companion_connector_executions (org_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_executions_idempotency_lookup
    ON companion_connector_executions (task_id, operation, review_request_id, idempotency_key)
    WHERE task_id IS NOT NULL
      AND review_request_id IS NOT NULL
      AND idempotency_key <> '';
