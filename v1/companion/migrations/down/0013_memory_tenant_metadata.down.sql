DROP INDEX IF EXISTS idx_memory_tenant_product;

DROP INDEX IF EXISTS idx_memory_scope_key;
CREATE UNIQUE INDEX IF NOT EXISTS idx_memory_scope_key
    ON companion_memory_entries (scope_type, scope_id, kind, key);

ALTER TABLE companion_memory_entries
    DROP COLUMN IF EXISTS retention_policy,
    DROP COLUMN IF EXISTS confidence,
    DROP COLUMN IF EXISTS provenance_json,
    DROP COLUMN IF EXISTS classification,
    DROP COLUMN IF EXISTS product_surface,
    DROP COLUMN IF EXISTS user_id,
    DROP COLUMN IF EXISTS org_id;
