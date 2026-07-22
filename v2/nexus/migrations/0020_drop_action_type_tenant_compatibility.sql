SET lock_timeout = '5s';
SET statement_timeout = '30s';

-- action_types historically carried both columns. Preserve the effective
-- organization value, then remove the obsolete compatibility boundary.
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema = current_schema()
          AND table_name = 'action_types'
          AND column_name = 'tenant_id'
    ) THEN
        UPDATE action_types
        SET org_id = tenant_id
        WHERE org_id IS NULL OR btrim(org_id) = '';

        ALTER TABLE action_types DROP CONSTRAINT IF EXISTS action_types_tenant_key_unique;
        DROP INDEX IF EXISTS idx_action_types_tenant_id;
        DROP INDEX IF EXISTS idx_action_types_tenant_key;
        DROP INDEX IF EXISTS idx_action_types_tenant_key_unique;
        ALTER TABLE action_types DROP COLUMN tenant_id;
    END IF;
END $$;

ALTER TABLE action_types ALTER COLUMN org_id SET NOT NULL;
ALTER TABLE action_types ALTER COLUMN org_id SET DEFAULT 'default';
CREATE UNIQUE INDEX IF NOT EXISTS idx_action_types_org_key_unique
    ON action_types (org_id, action_type_key);
CREATE INDEX IF NOT EXISTS idx_action_types_org_id
    ON action_types (org_id, id);
