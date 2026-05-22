ALTER TABLE companion_memory_entries
    ADD COLUMN IF NOT EXISTS org_id TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS user_id TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS product_surface TEXT NOT NULL DEFAULT 'companion',
    ADD COLUMN IF NOT EXISTS classification TEXT NOT NULL DEFAULT 'operational',
    ADD COLUMN IF NOT EXISTS provenance_json JSONB NOT NULL DEFAULT '{}',
    ADD COLUMN IF NOT EXISTS confidence DOUBLE PRECISION NOT NULL DEFAULT 1.0,
    ADD COLUMN IF NOT EXISTS retention_policy TEXT NOT NULL DEFAULT 'default';

UPDATE companion_memory_entries
SET org_id = scope_id
WHERE org_id = '' AND scope_type = 'org';

UPDATE companion_memory_entries
SET org_id = split_part(scope_id, ':', 1),
    user_id = split_part(scope_id, ':', 2)
WHERE org_id = '' AND scope_type = 'user' AND position(':' IN scope_id) > 0;

UPDATE companion_memory_entries m
SET org_id = t.org_id
FROM companion_tasks t
WHERE m.org_id = ''
  AND m.scope_type = 'task'
  AND m.scope_id = t.id::text;

UPDATE companion_memory_entries
SET classification = CASE
    WHEN kind IN ('user_preference', 'playbook_snippet') THEN 'stable'
    ELSE 'operational'
END
WHERE classification = 'operational';

CREATE INDEX IF NOT EXISTS idx_memory_tenant_product
    ON companion_memory_entries (org_id, product_surface, scope_type, scope_id, kind);

DROP INDEX IF EXISTS idx_memory_scope_key;
CREATE UNIQUE INDEX IF NOT EXISTS idx_memory_scope_key
    ON companion_memory_entries (org_id, product_surface, scope_type, scope_id, kind, key);
