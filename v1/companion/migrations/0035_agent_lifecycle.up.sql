ALTER TABLE companion_agents
    ADD COLUMN IF NOT EXISTS lifecycle_status TEXT NOT NULL DEFAULT 'active',
    ADD COLUMN IF NOT EXISTS origin_kind TEXT NOT NULL DEFAULT 'companion_fleet',
    ADD COLUMN IF NOT EXISTS review_status TEXT NOT NULL DEFAULT 'approved';

UPDATE companion_agents
SET lifecycle_status = 'archived'
WHERE status = 'disabled'
  AND lifecycle_status = 'active';

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'companion_agents_lifecycle_status_check'
    ) THEN
        ALTER TABLE companion_agents
            ADD CONSTRAINT companion_agents_lifecycle_status_check
            CHECK (lifecycle_status IN ('active', 'archived', 'trash'));
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'companion_agents_origin_kind_check'
    ) THEN
        ALTER TABLE companion_agents
            ADD CONSTRAINT companion_agents_origin_kind_check
            CHECK (origin_kind IN ('companion_fleet', 'runtime_inferred', 'manual'));
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'companion_agents_review_status_check'
    ) THEN
        ALTER TABLE companion_agents
            ADD CONSTRAINT companion_agents_review_status_check
            CHECK (review_status IN ('approved', 'needs_review', 'ignored'));
    END IF;
END $$;

CREATE INDEX IF NOT EXISTS idx_companion_agents_lifecycle
    ON companion_agents (org_id, product_surface, lifecycle_status, review_status);
