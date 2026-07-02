DROP INDEX IF EXISTS idx_memory_tenant_product_type;

ALTER TABLE companion_memory_entries
    DROP CONSTRAINT IF EXISTS companion_memory_entries_product_required,
    DROP CONSTRAINT IF EXISTS companion_memory_entries_org_required,
    DROP CONSTRAINT IF EXISTS companion_memory_entries_memory_type_check;

ALTER TABLE companion_memory_entries
    DROP CONSTRAINT IF EXISTS companion_memory_entries_kind_check,
    ADD CONSTRAINT companion_memory_entries_kind_check CHECK (kind IN (
        'task_summary', 'task_facts', 'playbook_snippet', 'user_preference'
    ));

ALTER TABLE companion_memory_entries
    DROP COLUMN IF EXISTS memory_type;
