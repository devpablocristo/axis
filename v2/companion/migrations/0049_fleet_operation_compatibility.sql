SET lock_timeout = '5s';
SET statement_timeout = '30s';

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema='public' AND table_name='companion_fleet_reconciliation_runs' AND column_name='trigger_type'
    ) AND NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema='public' AND table_name='companion_fleet_reconciliation_runs' AND column_name='trigger'
    ) THEN
        ALTER TABLE companion_fleet_reconciliation_runs RENAME COLUMN trigger_type TO trigger;
    END IF;
END $$;

CREATE TABLE IF NOT EXISTS companion_operation_requests (
    tenant_id text NOT NULL,
    actor_id text NOT NULL,
    idempotency_key text NOT NULL,
    operation text NOT NULL,
    resource_id text NOT NULL DEFAULT '',
    response_json jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, actor_id, idempotency_key)
);

ALTER TABLE companion_nexus_outbox DROP CONSTRAINT IF EXISTS companion_nexus_outbox_type_kind_check;
ALTER TABLE companion_nexus_outbox ADD CONSTRAINT companion_nexus_outbox_type_kind_check CHECK (
    (aggregate_type='execution_attempt' AND kind='execution_result') OR
    (aggregate_type='professional_authority' AND kind='audit_event') OR
    (aggregate_type='operational_finding' AND kind='operational_finding')
);
