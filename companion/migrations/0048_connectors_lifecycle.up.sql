ALTER TABLE companion_connectors
    ADD COLUMN IF NOT EXISTS status TEXT NOT NULL DEFAULT 'active',
    ADD COLUMN IF NOT EXISTS archived_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS trashed_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS version INTEGER NOT NULL DEFAULT 1;

UPDATE companion_connectors
SET status = CASE
    WHEN enabled THEN 'active'
    ELSE 'disabled'
END
WHERE status IS NULL OR status = '';

ALTER TABLE companion_connectors
    DROP CONSTRAINT IF EXISTS companion_connectors_status_check,
    ADD CONSTRAINT companion_connectors_status_check
        CHECK (status IN ('active', 'disabled', 'archived', 'trash'));

CREATE INDEX IF NOT EXISTS idx_connectors_org_status
    ON companion_connectors (org_id, status, updated_at DESC);

CREATE INDEX IF NOT EXISTS idx_connectors_archived
    ON companion_connectors (archived_at)
    WHERE archived_at IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_connectors_trash
    ON companion_connectors (trashed_at)
    WHERE trashed_at IS NOT NULL;
