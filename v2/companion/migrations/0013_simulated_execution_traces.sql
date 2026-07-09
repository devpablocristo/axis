ALTER TABLE companion_run_traces
    ADD COLUMN IF NOT EXISTS execution_result jsonb NULL;

ALTER TABLE companion_run_traces
    DROP CONSTRAINT IF EXISTS companion_run_traces_operation_check;

ALTER TABLE companion_run_traces
    ADD CONSTRAINT companion_run_traces_operation_check CHECK (
        operation IN ('dry_run', 'execution_gate', 'simulated_execution')
    );

CREATE INDEX IF NOT EXISTS idx_companion_run_traces_virployee_binding
    ON companion_run_traces (tenant_id, virployee_id, binding_hash, created_at DESC)
    WHERE binding_hash <> '';

CREATE INDEX IF NOT EXISTS idx_companion_run_traces_nexus_approval
    ON companion_run_traces (tenant_id, virployee_id, ((nexus_result->>'approval_id')), created_at DESC)
    WHERE nexus_result ? 'approval_id';

CREATE INDEX IF NOT EXISTS idx_companion_run_traces_execution_approval
    ON companion_run_traces (tenant_id, virployee_id, ((execution_result->>'approval_id')), created_at DESC)
    WHERE execution_result ? 'approval_id';
