DROP INDEX IF EXISTS idx_lifecycle_audit_tenant;
DROP INDEX IF EXISTS idx_lifecycle_audit_resource;
DROP TABLE IF EXISTS lifecycle_audit;
DROP INDEX IF EXISTS idx_assist_runs_idempotency;
ALTER TABLE assist_runs DROP COLUMN IF EXISTS idempotency_key;
