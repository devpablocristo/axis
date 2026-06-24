-- PR-0 (single-source initiative): idempotency for assist runs + a persistent
-- lifecycle audit trail (replaces the companion-side no-op audit).

-- Idempotency key for assist runs. Empty string = keyless (legacy behaviour).
ALTER TABLE assist_runs ADD COLUMN IF NOT EXISTS idempotency_key TEXT NOT NULL DEFAULT '';

-- At most one non-failed run per (org, key). Empty keys never participate, and
-- failed runs are excluded so a retry after a transient failure is not poisoned.
CREATE UNIQUE INDEX IF NOT EXISTS idx_assist_runs_idempotency
	ON assist_runs (org_id, idempotency_key)
	WHERE idempotency_key <> '' AND status <> 'failed';

-- Lifecycle audit trail for archive/restore/hard-delete. Mirrors the fields of
-- lifecycle.ArchiveAudit (platform/lifecycle v0.2.0): no from_state/to_state.
CREATE TABLE IF NOT EXISTS lifecycle_audit (
	id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
	tenant_id TEXT NOT NULL DEFAULT '',
	resource_type TEXT NOT NULL CHECK (btrim(resource_type) <> ''),
	resource_id UUID NOT NULL,
	action TEXT NOT NULL CHECK (btrim(action) <> ''),
	occurred_at TIMESTAMPTZ NOT NULL DEFAULT now(),
	actor TEXT NOT NULL DEFAULT '',
	reason TEXT,
	batch_id UUID,
	retention_expires TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_lifecycle_audit_resource
	ON lifecycle_audit (resource_type, resource_id, occurred_at DESC);

CREATE INDEX IF NOT EXISTS idx_lifecycle_audit_tenant
	ON lifecycle_audit (tenant_id, occurred_at DESC);
