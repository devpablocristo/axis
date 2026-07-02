-- Finaliza el modelo de memoria IA multi-tenant.
-- memory_type separa episodic/semantic/operational de classification
-- (sensibilidad/retención). Los CHECK NOT VALID bloquean nuevas escrituras
-- inseguras sin exigir que una base existente ya esté completamente saneada.

ALTER TABLE companion_memory_entries
    ADD COLUMN IF NOT EXISTS memory_type TEXT NOT NULL DEFAULT 'operational';

UPDATE companion_memory_entries
SET memory_type = CASE
    WHEN kind = 'episodic_event' THEN 'episodic'
    WHEN kind = 'semantic_fact' THEN 'semantic'
    WHEN kind = 'operational_state' THEN 'operational'
    WHEN kind = 'user_preference' THEN 'preference'
    WHEN kind = 'playbook_snippet' THEN 'playbook'
    WHEN kind IN ('task_summary', 'task_facts') THEN 'task_projection'
    ELSE 'operational'
END
WHERE memory_type = '' OR memory_type IS NULL OR memory_type = 'operational';

ALTER TABLE companion_memory_entries
    DROP CONSTRAINT IF EXISTS companion_memory_entries_kind_check,
    ADD CONSTRAINT companion_memory_entries_kind_check CHECK (kind IN (
        'task_summary', 'task_facts', 'playbook_snippet', 'user_preference',
        'episodic_event', 'semantic_fact', 'operational_state'
    ));

ALTER TABLE companion_memory_entries
    DROP CONSTRAINT IF EXISTS companion_memory_entries_memory_type_check,
    ADD CONSTRAINT companion_memory_entries_memory_type_check CHECK (memory_type IN (
        'episodic', 'semantic', 'operational', 'preference', 'playbook', 'task_projection'
    ));

ALTER TABLE companion_memory_entries
    DROP CONSTRAINT IF EXISTS companion_memory_entries_org_required,
    ADD CONSTRAINT companion_memory_entries_org_required CHECK (org_id <> '') NOT VALID,
    DROP CONSTRAINT IF EXISTS companion_memory_entries_product_required,
    ADD CONSTRAINT companion_memory_entries_product_required CHECK (product_surface <> '') NOT VALID;

CREATE INDEX IF NOT EXISTS idx_memory_tenant_product_type
    ON companion_memory_entries (org_id, product_surface, memory_type, updated_at DESC);
