SET lock_timeout = '5s';
SET statement_timeout = '30s';

-- Preserve already-operational installations. Any later governed-contract or
-- manifest change invalidates these compatibility rows and puts them through
-- the same gate as newly-created capabilities.
DO $$
DECLARE
    first_install boolean;
BEGIN
    SELECT NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema = current_schema()
          AND table_name = 'capabilities'
          AND column_name = 'promotion_state'
    ) INTO first_install;

    ALTER TABLE capabilities
        ADD COLUMN IF NOT EXISTS promotion_state text NOT NULL DEFAULT 'draft',
        ADD COLUMN IF NOT EXISTS manifest jsonb NOT NULL DEFAULT '{}'::jsonb,
        ADD COLUMN IF NOT EXISTS manifest_hash text NOT NULL DEFAULT '',
        ADD COLUMN IF NOT EXISTS conformed_hash text NOT NULL DEFAULT '',
        ADD COLUMN IF NOT EXISTS conformance_report jsonb NOT NULL DEFAULT '{"conformant":false,"checks":[]}'::jsonb,
        ADD COLUMN IF NOT EXISTS conformed_at timestamptz NULL,
        ADD COLUMN IF NOT EXISTS activated_at timestamptz NULL;

    IF first_install THEN
        UPDATE capabilities
        SET promotion_state = 'active',
            activated_at = COALESCE(activated_at, updated_at);
    END IF;
END$$;

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'capabilities_promotion_state_check') THEN
        ALTER TABLE capabilities
            ADD CONSTRAINT capabilities_promotion_state_check
            CHECK (promotion_state IN ('draft', 'conformant', 'active'));
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'capabilities_manifest_object_check') THEN
        ALTER TABLE capabilities
            ADD CONSTRAINT capabilities_manifest_object_check
            CHECK (jsonb_typeof(manifest) = 'object');
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'capabilities_conformance_report_object_check') THEN
        ALTER TABLE capabilities
            ADD CONSTRAINT capabilities_conformance_report_object_check
            CHECK (jsonb_typeof(conformance_report) = 'object');
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'capabilities_manifest_hash_check') THEN
        ALTER TABLE capabilities
            ADD CONSTRAINT capabilities_manifest_hash_check
            CHECK (manifest_hash = '' OR manifest_hash ~ '^[0-9a-f]{64}$');
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'capabilities_conformed_hash_check') THEN
        ALTER TABLE capabilities
            ADD CONSTRAINT capabilities_conformed_hash_check
            CHECK (conformed_hash = '' OR conformed_hash ~ '^[0-9a-f]{64}$');
    END IF;
END$$;

CREATE INDEX IF NOT EXISTS idx_capabilities_promotion
    ON capabilities (tenant_id, promotion_state, capability_key)
    WHERE archived_at IS NULL AND trashed_at IS NULL;
