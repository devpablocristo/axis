-- Tenant boundary for Companion tasks.

ALTER TABLE companion_tasks
    ADD COLUMN IF NOT EXISTS org_id TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_companion_tasks_org_updated
    ON companion_tasks (org_id, updated_at DESC);
