SET lock_timeout = '5s';
SET statement_timeout = '30s';

ALTER TABLE companion_assist_runs
    ADD COLUMN IF NOT EXISTS capability_key text NULL,
    ADD COLUMN IF NOT EXISTS capability_manifest_hash text NULL;

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'companion_assist_runs_capability_key_check') THEN
        ALTER TABLE companion_assist_runs
            ADD CONSTRAINT companion_assist_runs_capability_key_check
            CHECK (capability_key IS NULL OR capability_key ~ '^[a-z0-9_-]+\.[a-z0-9_-]+\.[a-z0-9_-]+$');
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'companion_assist_runs_capability_manifest_hash_check') THEN
        ALTER TABLE companion_assist_runs
            ADD CONSTRAINT companion_assist_runs_capability_manifest_hash_check
            CHECK (capability_manifest_hash IS NULL OR capability_manifest_hash ~ '^[0-9a-f]{64}$');
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'companion_assist_runs_capability_snapshot_check') THEN
        ALTER TABLE companion_assist_runs
            ADD CONSTRAINT companion_assist_runs_capability_snapshot_check
            CHECK ((capability_key IS NULL) = (capability_manifest_hash IS NULL));
    END IF;
END$$;

CREATE INDEX IF NOT EXISTS idx_companion_assist_runs_tenant_capability_started
    ON companion_assist_runs (tenant_id, capability_key, started_at DESC)
    WHERE capability_key IS NOT NULL;
