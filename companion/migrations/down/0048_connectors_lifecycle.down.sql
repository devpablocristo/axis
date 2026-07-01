DROP INDEX IF EXISTS idx_connectors_trash;
DROP INDEX IF EXISTS idx_connectors_archived;
DROP INDEX IF EXISTS idx_connectors_org_status;

ALTER TABLE companion_connectors
    DROP CONSTRAINT IF EXISTS companion_connectors_status_check,
    DROP COLUMN IF EXISTS version,
    DROP COLUMN IF EXISTS trashed_at,
    DROP COLUMN IF EXISTS archived_at,
    DROP COLUMN IF EXISTS status;
