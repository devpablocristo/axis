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

CREATE INDEX IF NOT EXISTS idx_companion_run_traces_virployee
    ON companion_run_traces (tenant_id, virployee_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_companion_run_traces_binding_hash
    ON companion_run_traces (tenant_id, binding_hash)
    WHERE binding_hash <> '';
