-- Persistencia de RunTrace del orquestador.
-- Captura identity chain, intent, autonomy, guardrail events y tool calls por cada Run del runtime.

CREATE TABLE IF NOT EXISTS companion_run_traces (
    run_id                 UUID PRIMARY KEY,
    org_id                 TEXT NOT NULL DEFAULT '',
    user_id                TEXT NOT NULL DEFAULT '',
    task_id                UUID,
    product_surface        TEXT NOT NULL DEFAULT '',
    intent                 TEXT NOT NULL DEFAULT '',
    autonomy_level         TEXT NOT NULL DEFAULT '',
    identity_chain_json    JSONB NOT NULL DEFAULT '{}'::jsonb,
    guardrail_events_json  JSONB NOT NULL DEFAULT '[]'::jsonb,
    tool_calls_json        JSONB NOT NULL DEFAULT '[]'::jsonb,
    error                  TEXT NOT NULL DEFAULT '',
    started_at             TIMESTAMPTZ NOT NULL,
    completed_at           TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_companion_run_traces_org_started
    ON companion_run_traces (org_id, started_at DESC);

CREATE INDEX IF NOT EXISTS idx_companion_run_traces_task
    ON companion_run_traces (task_id)
    WHERE task_id IS NOT NULL;
