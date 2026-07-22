SET lock_timeout = '5s';
SET statement_timeout = '30s';

ALTER TABLE governance_execution_results
    ADD COLUMN IF NOT EXISTS attestation_version text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS executor_version text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS attestation text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS result_hash text NOT NULL DEFAULT '';

-- Existing rows predate mandatory attestation. New writes are enforced by the
-- application and persist the complete, independently re-verifiable envelope.
CREATE INDEX IF NOT EXISTS governance_execution_results_attestation_idx
    ON governance_execution_results (tenant_id, governance_check_id, executor_version);
