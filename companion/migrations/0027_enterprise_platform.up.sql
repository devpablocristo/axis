-- Enterprise platform surfaces for semantic memory, capability catalog,
-- conformance, cost attribution and security evaluation reports.

DO $$
BEGIN
    CREATE EXTENSION IF NOT EXISTS vector;
EXCEPTION
    WHEN undefined_file THEN
        RAISE NOTICE 'pgvector extension is not installed; Companion will use json_vector fallback';
    WHEN insufficient_privilege THEN
        RAISE NOTICE 'pgvector extension cannot be created by this role; Companion will use json_vector fallback';
END $$;

CREATE TABLE IF NOT EXISTS companion_memory_vectors (
    memory_id         UUID NOT NULL,
    org_id            TEXT NOT NULL CHECK (btrim(org_id) <> ''),
    product_surface   TEXT NOT NULL DEFAULT 'companion',
    agent_id          TEXT NOT NULL DEFAULT '',
    namespace         TEXT NOT NULL CHECK (btrim(namespace) <> ''),
    embedding_model   TEXT NOT NULL CHECK (btrim(embedding_model) <> ''),
    embedding_backend TEXT NOT NULL DEFAULT 'json_vector',
    dims              INTEGER NOT NULL CHECK (dims > 0),
    content_hash      TEXT NOT NULL DEFAULT '',
    embedding_json    JSONB NOT NULL DEFAULT '[]'::jsonb,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (memory_id, namespace, embedding_model),
    FOREIGN KEY (memory_id) REFERENCES companion_memory_entries(id) ON DELETE CASCADE
);

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'vector') THEN
        EXECUTE 'ALTER TABLE companion_memory_vectors ADD COLUMN IF NOT EXISTS embedding_vector vector';
    END IF;
END $$;

CREATE INDEX IF NOT EXISTS idx_memory_vectors_namespace
    ON companion_memory_vectors (namespace, embedding_model, updated_at DESC);

CREATE INDEX IF NOT EXISTS idx_memory_vectors_org_surface
    ON companion_memory_vectors (org_id, product_surface, agent_id);

CREATE TABLE IF NOT EXISTS companion_memory_reviews (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id            TEXT NOT NULL CHECK (btrim(org_id) <> ''),
    product_surface   TEXT NOT NULL DEFAULT 'companion',
    memory_id         UUID REFERENCES companion_memory_entries(id) ON DELETE SET NULL,
    review_type       TEXT NOT NULL CHECK (review_type IN ('conflict', 'correction', 'invalidation', 'deletion')),
    status            TEXT NOT NULL DEFAULT 'open'
        CHECK (status IN ('open', 'approved', 'rejected', 'applied', 'cancelled')),
    reason            TEXT NOT NULL DEFAULT '',
    proposed_content  TEXT NOT NULL DEFAULT '',
    proposed_payload  JSONB NOT NULL DEFAULT '{}'::jsonb,
    decided_by        TEXT NOT NULL DEFAULT '',
    created_by        TEXT NOT NULL DEFAULT '',
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    decided_at        TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_memory_reviews_org_status
    ON companion_memory_reviews (org_id, product_surface, status, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_memory_reviews_memory
    ON companion_memory_reviews (memory_id, created_at DESC)
    WHERE memory_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS companion_capability_manifests (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    capability_id   TEXT NOT NULL CHECK (btrim(capability_id) <> ''),
    version         TEXT NOT NULL CHECK (btrim(version) <> ''),
    status          TEXT NOT NULL DEFAULT 'active'
        CHECK (status IN ('draft', 'active', 'deprecated')),
    source          TEXT NOT NULL DEFAULT 'generated'
        CHECK (source IN ('generated', 'imported')),
    manifest_json   JSONB NOT NULL,
    imported_by     TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (capability_id, version)
);

CREATE INDEX IF NOT EXISTS idx_capability_manifests_status
    ON companion_capability_manifests (status, capability_id, version);

CREATE TABLE IF NOT EXISTS companion_capability_conformance_runs (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id          TEXT NOT NULL DEFAULT '',
    capability_id   TEXT NOT NULL CHECK (btrim(capability_id) <> ''),
    version         TEXT NOT NULL CHECK (btrim(version) <> ''),
    status          TEXT NOT NULL CHECK (status IN ('passed', 'failed')),
    checks_json     JSONB NOT NULL DEFAULT '{}'::jsonb,
    errors_json     JSONB NOT NULL DEFAULT '[]'::jsonb,
    evidence_json   JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_by      TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_capability_conformance_runs_capability
    ON companion_capability_conformance_runs (capability_id, version, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_capability_conformance_runs_org
    ON companion_capability_conformance_runs (org_id, created_at DESC)
    WHERE org_id <> '';

CREATE TABLE IF NOT EXISTS companion_cost_events (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id               TEXT NOT NULL CHECK (btrim(org_id) <> ''),
    run_id               UUID,
    task_id              UUID,
    job_id               UUID,
    agent_id             TEXT NOT NULL DEFAULT '',
    capability_id        TEXT NOT NULL DEFAULT '',
    model                TEXT NOT NULL DEFAULT '',
    cost_class           TEXT NOT NULL DEFAULT '',
    event_type           TEXT NOT NULL CHECK (btrim(event_type) <> ''),
    estimated_tokens     BIGINT NOT NULL DEFAULT 0 CHECK (estimated_tokens >= 0),
    estimated_cost_cents BIGINT NOT NULL DEFAULT 0 CHECK (estimated_cost_cents >= 0),
    quantity             BIGINT NOT NULL DEFAULT 1 CHECK (quantity >= 0),
    payload_json         JSONB NOT NULL DEFAULT '{}'::jsonb,
    occurred_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_companion_cost_events_org_time
    ON companion_cost_events (org_id, occurred_at DESC);

CREATE INDEX IF NOT EXISTS idx_companion_cost_events_run
    ON companion_cost_events (run_id, occurred_at)
    WHERE run_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_companion_cost_events_agent
    ON companion_cost_events (org_id, agent_id, occurred_at DESC)
    WHERE agent_id <> '';

CREATE TABLE IF NOT EXISTS companion_security_eval_reports (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id          TEXT NOT NULL DEFAULT '',
    suite           TEXT NOT NULL CHECK (btrim(suite) <> ''),
    status          TEXT NOT NULL CHECK (status IN ('passed', 'failed')),
    score           DOUBLE PRECISION NOT NULL DEFAULT 0,
    threshold       DOUBLE PRECISION NOT NULL DEFAULT 0,
    report_json     JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_by      TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_security_eval_reports_suite_time
    ON companion_security_eval_reports (suite, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_security_eval_reports_org_time
    ON companion_security_eval_reports (org_id, created_at DESC)
    WHERE org_id <> '';
