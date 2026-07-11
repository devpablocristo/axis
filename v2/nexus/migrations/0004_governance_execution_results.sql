-- platform:migrate:non-transactional
SET lock_timeout = '5s';
SET statement_timeout = '30s';

CREATE TABLE IF NOT EXISTS governance_execution_results (
    id uuid PRIMARY KEY,
    tenant_id text NOT NULL,
    governance_check_id uuid NOT NULL REFERENCES governance_checks(id) ON DELETE CASCADE,
    idempotency_key text NOT NULL,
    request_fingerprint text NOT NULL,
    binding_hash text NOT NULL,
    status text NOT NULL,
    duration_ms bigint NOT NULL DEFAULT 0,
    result jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    UNIQUE (tenant_id, governance_check_id),
    UNIQUE (tenant_id, idempotency_key),
    CONSTRAINT governance_execution_results_status_check CHECK (status IN ('succeeded', 'failed'))
);
