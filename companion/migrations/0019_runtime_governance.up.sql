-- Runtime controls fundacional para Companion como empleado IA enterprise.
--
-- Esta capa no decide riesgo de acciones sensibles (eso sigue siendo Nexus).
-- Define controles operacionales por customer org: kill switch, autonomía máxima,
-- surfaces/modelos permitidos y budgets mensuales de inferencia/tools.

CREATE TABLE IF NOT EXISTS companion_tenant_runtime_policies (
    org_id                   TEXT PRIMARY KEY CHECK (btrim(org_id) <> ''),
    enabled                  BOOLEAN NOT NULL DEFAULT true,
    kill_switch              BOOLEAN NOT NULL DEFAULT false,
    max_autonomy             TEXT NOT NULL DEFAULT 'A2'
        CHECK (max_autonomy IN ('A0', 'A1', 'A2', 'A3', 'A4', 'A5')),
    allowed_product_surfaces TEXT[] NOT NULL DEFAULT ARRAY[]::TEXT[],
    allowed_models           TEXT[] NOT NULL DEFAULT ARRAY[]::TEXT[],
    monthly_token_budget     BIGINT NOT NULL DEFAULT 0 CHECK (monthly_token_budget >= 0),
    monthly_tool_call_budget BIGINT NOT NULL DEFAULT 0 CHECK (monthly_tool_call_budget >= 0),
    metadata_json            JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS companion_runtime_usage_monthly (
    org_id            TEXT NOT NULL CHECK (btrim(org_id) <> ''),
    period            TEXT NOT NULL CHECK (period ~ '^[0-9]{4}-[0-9]{2}$'),
    estimated_tokens  BIGINT NOT NULL DEFAULT 0 CHECK (estimated_tokens >= 0),
    llm_calls         BIGINT NOT NULL DEFAULT 0 CHECK (llm_calls >= 0),
    tool_calls        BIGINT NOT NULL DEFAULT 0 CHECK (tool_calls >= 0),
    tool_errors       BIGINT NOT NULL DEFAULT 0 CHECK (tool_errors >= 0),
    last_run_at       TIMESTAMPTZ,
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (org_id, period)
);

CREATE INDEX IF NOT EXISTS idx_companion_runtime_usage_period
    ON companion_runtime_usage_monthly (period, estimated_tokens DESC);

ALTER TABLE companion_run_traces
    ADD COLUMN IF NOT EXISTS usage_json JSONB NOT NULL DEFAULT '{}'::jsonb;
