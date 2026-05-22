-- Sprint 4 — Memoria conversacional propia del agente IA.
--
-- Estas tablas son el destino de la migración documentada en
-- pymes/ai/MEMORY_MIGRATION_PLAN.md:
--
--   ai_conversations                       → agent_conversations (+ messages)
--   ai_dossiers.memory.business_facts      → agent_memory_facts
--   ai_dossiers.memory.user_profiles       → agent_user_profiles
--   ai_dossiers.memory.recent_threads      → agent_conversation_messages
--   ai_dossiers.pending_action             → no migra (reemplazado por tasks)
--
-- Convención (alineada con 0015_tenant_fail_closed_constraints):
--   - org_id TEXT NOT NULL + CHECK(org_id <> '') NOT VALID
--   - timestamps TIMESTAMPTZ NOT NULL DEFAULT now()
--   - JSONB con default '{}'::jsonb
--   - source = 'companion_native' por default; 'pymes_ai_migrated' para data
--     importada en Sprint 5.

CREATE TABLE agent_conversations (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id          TEXT NOT NULL,
    user_id         TEXT NOT NULL DEFAULT '',
    product_surface TEXT NOT NULL,
    title           TEXT NOT NULL DEFAULT '',
    summary         TEXT NOT NULL DEFAULT '',
    source          TEXT NOT NULL DEFAULT 'companion_native',
    metadata_json   JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT agent_conversations_org_required CHECK (org_id <> '') NOT VALID,
    CONSTRAINT agent_conversations_product_required CHECK (product_surface <> '') NOT VALID
);

CREATE INDEX idx_agent_conversations_org_user ON agent_conversations (org_id, user_id, updated_at DESC);
CREATE INDEX idx_agent_conversations_source ON agent_conversations (source);

CREATE TABLE agent_conversation_messages (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    conversation_id UUID NOT NULL REFERENCES agent_conversations (id) ON DELETE CASCADE,
    org_id          TEXT NOT NULL,
    role            TEXT NOT NULL,
    content         TEXT NOT NULL DEFAULT '',
    metadata_json   JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT agent_conversation_messages_org_required CHECK (org_id <> '') NOT VALID,
    CONSTRAINT agent_conversation_messages_role_check CHECK (
        role IN ('user', 'assistant', 'system', 'tool')
    )
);

CREATE INDEX idx_agent_conversation_messages_conv
    ON agent_conversation_messages (conversation_id, created_at);

CREATE TABLE agent_memory_facts (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id          TEXT NOT NULL,
    conversation_id UUID REFERENCES agent_conversations (id) ON DELETE SET NULL,
    fact_key        TEXT NOT NULL,
    fact_value      JSONB NOT NULL DEFAULT '{}'::jsonb,
    confidence      NUMERIC(5,4) NOT NULL DEFAULT 1.0,
    source          TEXT NOT NULL DEFAULT 'companion_native',
    metadata_json   JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT agent_memory_facts_org_required CHECK (org_id <> '') NOT VALID,
    CONSTRAINT agent_memory_facts_key_required CHECK (fact_key <> ''),
    CONSTRAINT agent_memory_facts_confidence_range CHECK (confidence >= 0 AND confidence <= 1)
);

-- Una org no puede tener el mismo fact_key dos veces; upsert lo actualiza.
CREATE UNIQUE INDEX idx_agent_memory_facts_org_key
    ON agent_memory_facts (org_id, fact_key);
CREATE INDEX idx_agent_memory_facts_conv ON agent_memory_facts (conversation_id);

CREATE TABLE agent_user_profiles (
    org_id        TEXT NOT NULL,
    user_id       TEXT NOT NULL,
    profile_json  JSONB NOT NULL DEFAULT '{}'::jsonb,
    source        TEXT NOT NULL DEFAULT 'companion_native',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (org_id, user_id),
    CONSTRAINT agent_user_profiles_org_required CHECK (org_id <> ''),
    CONSTRAINT agent_user_profiles_user_required CHECK (user_id <> '')
);
