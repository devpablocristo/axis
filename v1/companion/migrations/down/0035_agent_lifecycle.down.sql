DROP INDEX IF EXISTS idx_companion_agents_lifecycle;

ALTER TABLE companion_agents
    DROP CONSTRAINT IF EXISTS companion_agents_review_status_check,
    DROP CONSTRAINT IF EXISTS companion_agents_origin_kind_check,
    DROP CONSTRAINT IF EXISTS companion_agents_lifecycle_status_check,
    DROP COLUMN IF EXISTS review_status,
    DROP COLUMN IF EXISTS origin_kind,
    DROP COLUMN IF EXISTS lifecycle_status;
