-- Flota de empleados IA por customer org/product surface.

CREATE TABLE IF NOT EXISTS companion_agents (
    org_id                TEXT NOT NULL CHECK (btrim(org_id) <> ''),
    product_surface       TEXT NOT NULL DEFAULT 'companion',
    agent_id              TEXT NOT NULL CHECK (btrim(agent_id) <> ''),
    display_name          TEXT NOT NULL DEFAULT '',
    role                  TEXT NOT NULL DEFAULT '',
    profile_id            TEXT NOT NULL DEFAULT '',
    status                TEXT NOT NULL DEFAULT 'active'
        CHECK (status IN ('active', 'disabled')),
    max_autonomy          TEXT NOT NULL DEFAULT 'A2',
    allowed_tools         TEXT[] NOT NULL DEFAULT '{}',
    allowed_capabilities  TEXT[] NOT NULL DEFAULT '{}',
    allowed_connectors    TEXT[] NOT NULL DEFAULT '{}',
    memory_scope_id       TEXT NOT NULL DEFAULT '',
    shared_memory_policy  JSONB NOT NULL DEFAULT '{}'::jsonb,
    limits_json           JSONB NOT NULL DEFAULT '{}'::jsonb,
    sla_json              JSONB NOT NULL DEFAULT '{}'::jsonb,
    metadata_json         JSONB NOT NULL DEFAULT '{}'::jsonb,
    version               BIGINT NOT NULL DEFAULT 1,
    created_by            TEXT NOT NULL DEFAULT '',
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (org_id, product_surface, agent_id)
);

CREATE INDEX IF NOT EXISTS idx_companion_agents_org_status
    ON companion_agents (org_id, product_surface, status);

CREATE TABLE IF NOT EXISTS companion_agent_handoffs (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id          TEXT NOT NULL CHECK (btrim(org_id) <> ''),
    product_surface TEXT NOT NULL DEFAULT 'companion',
    task_id         TEXT NOT NULL DEFAULT '',
    from_agent_id   TEXT NOT NULL CHECK (btrim(from_agent_id) <> ''),
    to_agent_id     TEXT NOT NULL CHECK (btrim(to_agent_id) <> ''),
    status          TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'accepted', 'rejected', 'completed', 'cancelled')),
    reason          TEXT NOT NULL DEFAULT '',
    context_json    JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_by      TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_companion_agent_handoffs_org_time
    ON companion_agent_handoffs (org_id, product_surface, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_companion_agent_handoffs_task
    ON companion_agent_handoffs (org_id, product_surface, task_id)
    WHERE task_id <> '';

CREATE TABLE IF NOT EXISTS companion_agent_audit (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id          TEXT NOT NULL CHECK (btrim(org_id) <> ''),
    product_surface TEXT NOT NULL DEFAULT 'companion',
    agent_id        TEXT NOT NULL DEFAULT '',
    action          TEXT NOT NULL CHECK (btrim(action) <> ''),
    changed_by      TEXT NOT NULL DEFAULT '',
    payload_json    JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_companion_agent_audit_org_time
    ON companion_agent_audit (org_id, product_surface, created_at DESC);
