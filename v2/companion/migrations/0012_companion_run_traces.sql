-- platform:migrate:non-transactional
SET lock_timeout = '5s';
SET statement_timeout = '30s';

CREATE TABLE IF NOT EXISTS companion_run_traces (
    id uuid PRIMARY KEY,
    tenant_id text NOT NULL DEFAULT 'default',
    virployee_id uuid NOT NULL REFERENCES virployees(id) ON DELETE CASCADE,
    operation text NOT NULL,
    input_hash text NOT NULL,
    input_preview text NOT NULL DEFAULT '',
    intent jsonb NOT NULL DEFAULT '{}'::jsonb,
    capability_id uuid NULL REFERENCES capabilities(id),
    capability_key text NOT NULL DEFAULT '',
    dry_run_decision text NOT NULL DEFAULT '',
    gate_decision text NULL,
    gate_checks jsonb NOT NULL DEFAULT '[]'::jsonb,
    nexus_result jsonb NULL,
    binding_hash text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL,
    CONSTRAINT companion_run_traces_operation_check CHECK (
        operation IN ('dry_run', 'execution_gate')
    )
);

ALTER TABLE companion_run_traces
    ADD COLUMN IF NOT EXISTS tenant_id text NOT NULL DEFAULT 'default';

ALTER TABLE companion_run_traces
    ADD COLUMN IF NOT EXISTS virployee_id uuid;

ALTER TABLE companion_run_traces
    ADD COLUMN IF NOT EXISTS operation text NOT NULL DEFAULT 'dry_run';

ALTER TABLE companion_run_traces
    ADD COLUMN IF NOT EXISTS input_hash text NOT NULL DEFAULT '';

ALTER TABLE companion_run_traces
    ADD COLUMN IF NOT EXISTS input_preview text NOT NULL DEFAULT '';

ALTER TABLE companion_run_traces
    ADD COLUMN IF NOT EXISTS intent jsonb NOT NULL DEFAULT '{}'::jsonb;

ALTER TABLE companion_run_traces
    ADD COLUMN IF NOT EXISTS capability_id uuid NULL;

ALTER TABLE companion_run_traces
    ADD COLUMN IF NOT EXISTS capability_key text NOT NULL DEFAULT '';

ALTER TABLE companion_run_traces
    ADD COLUMN IF NOT EXISTS dry_run_decision text NOT NULL DEFAULT '';

ALTER TABLE companion_run_traces
    ADD COLUMN IF NOT EXISTS gate_decision text NULL;

ALTER TABLE companion_run_traces
    ADD COLUMN IF NOT EXISTS gate_checks jsonb NOT NULL DEFAULT '[]'::jsonb;

ALTER TABLE companion_run_traces
    ADD COLUMN IF NOT EXISTS nexus_result jsonb NULL;

ALTER TABLE companion_run_traces
    ADD COLUMN IF NOT EXISTS execution_result jsonb NULL;

ALTER TABLE companion_run_traces
    ADD COLUMN IF NOT EXISTS binding_hash text NOT NULL DEFAULT '';

ALTER TABLE companion_run_traces
    ADD COLUMN IF NOT EXISTS created_at timestamptz NOT NULL DEFAULT now();

UPDATE companion_run_traces
SET operation = 'dry_run'
WHERE operation NOT IN ('dry_run', 'execution_gate', 'simulated_execution')
    OR btrim(operation) = '';

UPDATE companion_run_traces traces
SET tenant_id = virployees.tenant_id
FROM virployees
WHERE traces.virployee_id = virployees.id
    AND traces.tenant_id = 'default';

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_companion_run_traces_virployee
    ON companion_run_traces (tenant_id, virployee_id, created_at DESC);

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_companion_run_traces_binding_hash
    ON companion_run_traces (tenant_id, binding_hash)
    WHERE binding_hash <> '';
