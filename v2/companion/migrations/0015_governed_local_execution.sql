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

CREATE TABLE IF NOT EXISTS companion_prepared_actions (
    id uuid PRIMARY KEY,
    tenant_id text NOT NULL,
    virployee_id uuid NOT NULL REFERENCES virployees(id) ON DELETE CASCADE,
    governance_check_id uuid NOT NULL,
    approval_id uuid NOT NULL,
    capability_key text NOT NULL,
    action text NOT NULL,
    payload jsonb NOT NULL,
    payload_hash text NOT NULL,
    binding_hash text NOT NULL,
    created_at timestamptz NOT NULL,
    UNIQUE (tenant_id, approval_id),
    UNIQUE (tenant_id, governance_check_id)
);

CREATE TABLE IF NOT EXISTS companion_execution_attempts (
    id uuid PRIMARY KEY,
    tenant_id text NOT NULL,
    virployee_id uuid NOT NULL REFERENCES virployees(id) ON DELETE CASCADE,
    prepared_action_id uuid NOT NULL REFERENCES companion_prepared_actions(id) ON DELETE CASCADE,
    idempotency_key text NOT NULL,
    status text NOT NULL,
    resource_id text NOT NULL DEFAULT '',
    result jsonb NOT NULL DEFAULT '{}'::jsonb,
    error text NOT NULL DEFAULT '',
    duration_ms bigint NOT NULL DEFAULT 0,
    nexus_report_status text NOT NULL DEFAULT 'pending',
    started_at timestamptz NOT NULL,
    completed_at timestamptz NULL,
    updated_at timestamptz NOT NULL,
    UNIQUE (tenant_id, idempotency_key),
    UNIQUE (tenant_id, prepared_action_id),
    CONSTRAINT companion_execution_attempts_status_check CHECK (status IN ('running', 'succeeded', 'failed')),
    CONSTRAINT companion_execution_attempts_report_status_check CHECK (nexus_report_status IN ('pending', 'reported', 'failed'))
);

CREATE TABLE IF NOT EXISTS companion_local_calendar_events (
    id uuid PRIMARY KEY,
    tenant_id text NOT NULL,
    virployee_id uuid NOT NULL REFERENCES virployees(id) ON DELETE CASCADE,
    execution_attempt_id uuid NOT NULL REFERENCES companion_execution_attempts(id) ON DELETE CASCADE,
    idempotency_key text NOT NULL,
    title text NOT NULL,
    starts_at timestamptz NOT NULL,
    timezone text NOT NULL,
    -- squawk-ignore prefer-bigint-over-int
    duration_minutes integer NOT NULL,
    attendees jsonb NOT NULL DEFAULT '[]'::jsonb,
    created_at timestamptz NOT NULL,
    UNIQUE (tenant_id, idempotency_key),
    UNIQUE (execution_attempt_id),
    CONSTRAINT companion_local_calendar_events_duration_check CHECK (duration_minutes BETWEEN 5 AND 1440)
);

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_companion_execution_attempts_virployee
    ON companion_execution_attempts (tenant_id, virployee_id, started_at DESC);
