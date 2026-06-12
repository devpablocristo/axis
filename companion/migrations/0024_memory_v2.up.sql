-- Memory v2: provenance, trust, supersession, conflict workflow, poisoning
-- flags and deterministic embedding namespace per customer org.

ALTER TABLE companion_memory_entries
    ADD COLUMN IF NOT EXISTS status TEXT NOT NULL DEFAULT 'active',
    ADD COLUMN IF NOT EXISTS source TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS trust_score DOUBLE PRECISION NOT NULL DEFAULT 1.0,
    ADD COLUMN IF NOT EXISTS embedding_namespace TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS embedding_model TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS embedding_json JSONB NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN IF NOT EXISTS supersedes_id UUID,
    ADD COLUMN IF NOT EXISTS superseded_by_id UUID,
    ADD COLUMN IF NOT EXISTS conflict_group_id UUID,
    ADD COLUMN IF NOT EXISTS last_verified_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS confidence_decay_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS poisoning_flags TEXT[] NOT NULL DEFAULT ARRAY[]::TEXT[];

UPDATE companion_memory_entries
SET embedding_namespace = org_id || ':' || product_surface
WHERE embedding_namespace = '';

ALTER TABLE companion_memory_entries
    DROP CONSTRAINT IF EXISTS companion_memory_entries_status_check,
    ADD CONSTRAINT companion_memory_entries_status_check CHECK (status IN (
        'active', 'superseded', 'conflict', 'rejected', 'forgotten'
    ));

ALTER TABLE companion_memory_entries
    DROP CONSTRAINT IF EXISTS companion_memory_entries_kind_check,
    ADD CONSTRAINT companion_memory_entries_kind_check CHECK (kind IN (
        'task_summary', 'task_facts', 'playbook_snippet', 'user_preference',
        'episodic_event', 'semantic_fact', 'operational_state',
        'tenant_knowledge', 'business_context', 'procedure'
    ));

ALTER TABLE companion_memory_entries
    DROP CONSTRAINT IF EXISTS companion_memory_entries_memory_type_check,
    ADD CONSTRAINT companion_memory_entries_memory_type_check CHECK (memory_type IN (
        'episodic', 'semantic', 'operational', 'preference', 'playbook',
        'task_projection', 'tenant_knowledge', 'business_context', 'procedural'
    ));

CREATE INDEX IF NOT EXISTS idx_memory_v2_namespace_type_status
    ON companion_memory_entries (embedding_namespace, memory_type, status, updated_at DESC);

CREATE INDEX IF NOT EXISTS idx_memory_v2_conflicts
    ON companion_memory_entries (org_id, product_surface, conflict_group_id)
    WHERE conflict_group_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS companion_memory_audit (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    memory_id       UUID NOT NULL,
    org_id          TEXT NOT NULL CHECK (btrim(org_id) <> ''),
    product_surface TEXT NOT NULL DEFAULT 'companion',
    action          TEXT NOT NULL CHECK (btrim(action) <> ''),
    status          TEXT NOT NULL DEFAULT '',
    payload_json    JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_memory_audit_memory_created
    ON companion_memory_audit (memory_id, created_at);

CREATE INDEX IF NOT EXISTS idx_memory_audit_org_created
    ON companion_memory_audit (org_id, created_at DESC);

CREATE TABLE IF NOT EXISTS companion_memory_summaries (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id          TEXT NOT NULL CHECK (btrim(org_id) <> ''),
    product_surface TEXT NOT NULL DEFAULT 'companion',
    scope_type      TEXT NOT NULL,
    scope_id        TEXT NOT NULL,
    summary_type    TEXT NOT NULL DEFAULT 'compaction',
    version         INTEGER NOT NULL DEFAULT 1,
    content_text    TEXT NOT NULL DEFAULT '',
    source_count    INTEGER NOT NULL DEFAULT 0,
    payload_json    JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_memory_summaries_scope_version
    ON companion_memory_summaries (org_id, product_surface, scope_type, scope_id, summary_type, version);
