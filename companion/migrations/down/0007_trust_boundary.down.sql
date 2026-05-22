-- Reversa de 0007_trust_boundary.up.sql
DROP INDEX IF EXISTS idx_executions_idempotency_lookup;
DROP INDEX IF EXISTS idx_executions_org_created;
DROP INDEX IF EXISTS idx_connectors_org_kind;

ALTER TABLE companion_connector_executions
    DROP COLUMN IF EXISTS evidence_json,
    DROP COLUMN IF EXISTS idempotency_key,
    DROP COLUMN IF EXISTS actor_id,
    DROP COLUMN IF EXISTS org_id;

ALTER TABLE companion_connectors
    DROP COLUMN IF EXISTS org_id;
