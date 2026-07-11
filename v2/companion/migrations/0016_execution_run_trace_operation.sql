-- platform:migrate:non-transactional
SET lock_timeout = '5s';
SET statement_timeout = '30s';

ALTER TABLE companion_run_traces
    DROP CONSTRAINT IF EXISTS companion_run_traces_operation_check;

ALTER TABLE companion_run_traces
    ADD CONSTRAINT companion_run_traces_operation_check CHECK (
        operation IN ('dry_run', 'execution_gate', 'simulated_execution', 'execution')
    ) NOT VALID;

ALTER TABLE companion_run_traces
    VALIDATE CONSTRAINT companion_run_traces_operation_check;
