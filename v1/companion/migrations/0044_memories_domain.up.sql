CREATE TABLE IF NOT EXISTS companion_memories (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id         UUID NOT NULL,
    org_id            TEXT NOT NULL,
    product_surface   TEXT NOT NULL,
    owner_virployee_id UUID,
    policy_json       JSONB NOT NULL DEFAULT jsonb_build_object(
        'enabled_by_default', true,
        'retention_days', 365,
        'allow_user_memory', true,
        'allow_task_memory', true,
        'allow_tenant_memory', true
    ),
    status            TEXT NOT NULL DEFAULT 'active',
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    archived_at       TIMESTAMPTZ,
    version           INTEGER NOT NULL DEFAULT 1,
    CONSTRAINT companion_memories_org_required CHECK (org_id <> ''),
    CONSTRAINT companion_memories_product_required CHECK (product_surface <> ''),
    CONSTRAINT companion_memories_status_check CHECK (status IN ('active', 'disabled', 'archived')),
    CONSTRAINT companion_memories_policy_object_check CHECK (jsonb_typeof(policy_json) = 'object')
);

CREATE INDEX IF NOT EXISTS idx_companion_memories_tenant_status
    ON companion_memories (tenant_id, status, updated_at DESC);

CREATE INDEX IF NOT EXISTS idx_companion_memories_org_surface_status
    ON companion_memories (org_id, product_surface, status, updated_at DESC);

CREATE TABLE IF NOT EXISTS companion_memory_container_entries (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    memory_id    UUID NOT NULL REFERENCES companion_memories(id) ON DELETE CASCADE,
    kind         TEXT NOT NULL,
    content_text TEXT NOT NULL DEFAULT '',
    confidence   DOUBLE PRECISION NOT NULL DEFAULT 1,
    status       TEXT NOT NULL DEFAULT 'active',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT companion_memory_container_entries_kind_required CHECK (kind <> ''),
    CONSTRAINT companion_memory_container_entries_confidence_check CHECK (confidence >= 0 AND confidence <= 1),
    CONSTRAINT companion_memory_container_entries_status_check CHECK (status IN ('active', 'archived'))
);

CREATE INDEX IF NOT EXISTS idx_companion_memory_container_entries_memory
    ON companion_memory_container_entries (memory_id, updated_at DESC);

CREATE TABLE IF NOT EXISTS companion_memory_container_audit (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    memory_id   UUID NOT NULL REFERENCES companion_memories(id) ON DELETE CASCADE,
    tenant_id   UUID NOT NULL,
    actor_id    TEXT NOT NULL DEFAULT '',
    action      TEXT NOT NULL,
    status      TEXT NOT NULL,
    snapshot    JSONB NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT companion_memory_container_audit_action_required CHECK (action <> '')
);

CREATE INDEX IF NOT EXISTS idx_companion_memory_container_audit_memory
    ON companion_memory_container_audit (memory_id, created_at DESC);
