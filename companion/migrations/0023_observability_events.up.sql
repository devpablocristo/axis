-- Ledger de observabilidad operacional para replay de runs y acciones.

CREATE TABLE IF NOT EXISTS companion_observability_events (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id        TEXT NOT NULL CHECK (btrim(org_id) <> ''),
    run_id        UUID,
    task_id       UUID,
    job_id        UUID,
    agent_id      TEXT NOT NULL DEFAULT '',
    capability_id TEXT NOT NULL DEFAULT '',
    event_type    TEXT NOT NULL CHECK (btrim(event_type) <> ''),
    event_name    TEXT NOT NULL CHECK (btrim(event_name) <> ''),
    severity      TEXT NOT NULL DEFAULT 'info',
    trace_id      TEXT NOT NULL DEFAULT '',
    payload_json  JSONB NOT NULL DEFAULT '{}'::jsonb,
    redacted      BOOLEAN NOT NULL DEFAULT true,
    occurred_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_companion_observability_run
    ON companion_observability_events (run_id, occurred_at)
    WHERE run_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_companion_observability_org_time
    ON companion_observability_events (org_id, occurred_at DESC);

CREATE INDEX IF NOT EXISTS idx_companion_observability_task
    ON companion_observability_events (task_id, occurred_at)
    WHERE task_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_companion_observability_job
    ON companion_observability_events (job_id, occurred_at)
    WHERE job_id IS NOT NULL;
