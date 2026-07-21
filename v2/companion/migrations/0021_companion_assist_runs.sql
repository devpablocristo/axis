SET lock_timeout = '5s';
SET statement_timeout = '30s';

-- companion_assist_runs records each product "assist" run (process-and-respond):
-- a product sends input, a virployee interprets it and answers. The run is the
-- accountability trace (run_id) returned to the product. reserve-before-LLM: the
-- row is inserted 'running' before the model call, so concurrent retries collide
-- on the unique key instead of invoking the model twice.
CREATE TABLE IF NOT EXISTS companion_assist_runs (
    id uuid PRIMARY KEY,
    tenant_id text NOT NULL,
    virployee_id uuid NOT NULL REFERENCES virployees(id) ON DELETE CASCADE,
    assist_type text NOT NULL DEFAULT '',
    idempotency_key text NOT NULL,
    status text NOT NULL,
    input_hash text NOT NULL DEFAULT '',
    input_preview text NOT NULL DEFAULT '',
    output jsonb NOT NULL DEFAULT '{}'::jsonb,
    output_text text NOT NULL DEFAULT '',
    answered boolean NOT NULL DEFAULT false,
    degraded boolean NOT NULL DEFAULT false,
    model text NOT NULL DEFAULT '',
    prompt_version text NOT NULL DEFAULT '',
    error text NOT NULL DEFAULT '',
    duration_ms bigint NOT NULL DEFAULT 0,
    started_at timestamptz NOT NULL,
    completed_at timestamptz NULL,
    UNIQUE (tenant_id, virployee_id, idempotency_key),
    CONSTRAINT companion_assist_runs_status_check CHECK (status IN ('running', 'done', 'failed'))
);

CREATE INDEX IF NOT EXISTS idx_companion_assist_runs_virployee
    ON companion_assist_runs (tenant_id, virployee_id, started_at DESC);
