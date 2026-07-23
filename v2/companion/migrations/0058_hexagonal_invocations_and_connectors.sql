SET lock_timeout = '5s';
SET statement_timeout = '30s';

ALTER TABLE companion_assist_runs
    ADD COLUMN IF NOT EXISTS invocation_context_version text NOT NULL DEFAULT 'legacy.v1',
    ADD COLUMN IF NOT EXISTS integration_id text NULL,
    ADD COLUMN IF NOT EXISTS integration_revision bigint NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS integration_hash text NULL,
    ADD COLUMN IF NOT EXISTS principal_type text NULL,
    ADD COLUMN IF NOT EXISTS principal_id text NULL,
    ADD COLUMN IF NOT EXISTS principal_scopes text[] NOT NULL DEFAULT '{}',
    ADD COLUMN IF NOT EXISTS access_mode text NOT NULL DEFAULT 'direct',
    ADD COLUMN IF NOT EXISTS capability_id uuid NULL;

ALTER TABLE companion_assist_runs
    ALTER COLUMN invocation_context_version SET DEFAULT 'axis.invocation-context.v1';

UPDATE companion_assist_runs AS run
SET capability_id = capability.id
FROM capabilities AS capability
WHERE run.capability_id IS NULL
  AND run.capability_key IS NOT NULL
  AND capability.org_id = run.org_id
  AND capability.capability_key = run.capability_key;

ALTER TABLE companion_assist_runs
    DROP CONSTRAINT IF EXISTS companion_assist_runs_capability_id_fkey;
ALTER TABLE companion_assist_runs
    ADD CONSTRAINT companion_assist_runs_capability_id_fkey
    FOREIGN KEY (capability_id) REFERENCES capabilities(id) ON DELETE SET NULL NOT VALID;
ALTER TABLE companion_assist_runs
    VALIDATE CONSTRAINT companion_assist_runs_capability_id_fkey;

ALTER TABLE companion_assist_runs
    DROP CONSTRAINT IF EXISTS companion_assist_runs_invocation_binding_check;
ALTER TABLE companion_assist_runs
    ADD CONSTRAINT companion_assist_runs_invocation_binding_check CHECK (
        (
            integration_id IS NULL
            AND integration_revision = 0
            AND integration_hash IS NULL
        ) OR (
            btrim(product_id) <> ''
            AND btrim(integration_id) <> ''
            AND integration_revision > 0
            AND integration_hash ~ '^[0-9a-f]{64}$'
        )
    ) NOT VALID;
ALTER TABLE companion_assist_runs
    VALIDATE CONSTRAINT companion_assist_runs_invocation_binding_check;

ALTER TABLE companion_assist_runs
    DROP CONSTRAINT IF EXISTS companion_assist_runs_principal_binding_check;
ALTER TABLE companion_assist_runs
    ADD CONSTRAINT companion_assist_runs_principal_binding_check
    CHECK ((principal_type IS NULL) = (principal_id IS NULL)) NOT VALID;
ALTER TABLE companion_assist_runs
    VALIDATE CONSTRAINT companion_assist_runs_principal_binding_check;

ALTER TABLE companion_assist_runs
    DROP CONSTRAINT IF EXISTS companion_assist_runs_access_mode_check;
ALTER TABLE companion_assist_runs
    ADD CONSTRAINT companion_assist_runs_access_mode_check
    CHECK (access_mode IN ('direct','via_orchestrator','via_companion')) NOT VALID;
ALTER TABLE companion_assist_runs
    VALIDATE CONSTRAINT companion_assist_runs_access_mode_check;

CREATE INDEX IF NOT EXISTS idx_companion_assist_runs_org_capability_id
    ON companion_assist_runs (org_id, capability_id, started_at DESC)
    WHERE capability_id IS NOT NULL;

ALTER TABLE companion_prepared_actions
    ADD COLUMN IF NOT EXISTS capability_id uuid NULL,
    ADD COLUMN IF NOT EXISTS payload_version text NOT NULL DEFAULT 'legacy.v1',
    ADD COLUMN IF NOT EXISTS executor_binding_id text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS operation text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS governance_policy_snapshot_hash text NOT NULL DEFAULT '';

UPDATE companion_prepared_actions
SET payload_version = COALESCE(NULLIF(payload->>'schema_version',''), 'legacy.v1')
WHERE payload_version = 'legacy.v1';

UPDATE companion_prepared_actions AS prepared
SET capability_id = capability.id
FROM capabilities AS capability
WHERE prepared.capability_id IS NULL
  AND capability.org_id = prepared.org_id
  AND capability.capability_key = prepared.capability_key;

UPDATE companion_prepared_actions
SET governance_policy_snapshot_hash = nexus_policy_snapshot_hash
WHERE governance_policy_snapshot_hash = ''
  AND nexus_policy_snapshot_hash <> '';

ALTER TABLE companion_prepared_actions
    DROP CONSTRAINT IF EXISTS companion_prepared_actions_capability_id_fkey;
ALTER TABLE companion_prepared_actions
    ADD CONSTRAINT companion_prepared_actions_capability_id_fkey
    FOREIGN KEY (capability_id) REFERENCES capabilities(id) ON DELETE SET NULL NOT VALID;
ALTER TABLE companion_prepared_actions
    VALIDATE CONSTRAINT companion_prepared_actions_capability_id_fkey;

ALTER TABLE companion_prepared_actions
    DROP CONSTRAINT IF EXISTS companion_prepared_actions_v2_binding_check;
ALTER TABLE companion_prepared_actions
    ADD CONSTRAINT companion_prepared_actions_v2_binding_check CHECK (
        payload_version <> 'axis.prepared-action.v2' OR (
            capability_id IS NOT NULL
            AND btrim(executor_binding_id) <> ''
            AND btrim(operation) <> ''
            AND payload->>'capability_id' = capability_id::text
            AND payload->>'executor_binding_id' = executor_binding_id
            AND payload->>'operation' = operation
        )
    ) NOT VALID;
ALTER TABLE companion_prepared_actions
    VALIDATE CONSTRAINT companion_prepared_actions_v2_binding_check;

ALTER TABLE companion_execution_attempts
    ADD COLUMN IF NOT EXISTS governance_report_status text NOT NULL DEFAULT 'pending';

UPDATE companion_execution_attempts
SET governance_report_status = nexus_report_status
WHERE governance_report_status = 'pending'
  AND nexus_report_status <> 'pending';

ALTER TABLE companion_execution_attempts
    DROP CONSTRAINT IF EXISTS companion_execution_attempts_governance_report_status_check;
ALTER TABLE companion_execution_attempts
    ADD CONSTRAINT companion_execution_attempts_governance_report_status_check
    CHECK (governance_report_status IN ('pending','reported','failed','dead')) NOT VALID;
ALTER TABLE companion_execution_attempts
    VALIDATE CONSTRAINT companion_execution_attempts_governance_report_status_check;

ALTER TABLE companion_nexus_outbox
    ADD COLUMN IF NOT EXISTS destination text NOT NULL DEFAULT 'governance',
    ADD COLUMN IF NOT EXISTS contract_version text NOT NULL DEFAULT 'axis.governance-outbox.v1';

ALTER TABLE companion_nexus_outbox
    DROP CONSTRAINT IF EXISTS companion_nexus_outbox_destination_check;
ALTER TABLE companion_nexus_outbox
    ADD CONSTRAINT companion_nexus_outbox_destination_check
    CHECK (destination ~ '^[a-z][a-z0-9._-]{0,63}$') NOT VALID;
ALTER TABLE companion_nexus_outbox
    VALIDATE CONSTRAINT companion_nexus_outbox_destination_check;

CREATE OR REPLACE VIEW companion_outbox AS
SELECT id, org_id, destination, contract_version, aggregate_type, aggregate_id,
       kind, dedupe_key, payload_json, status, attempts, max_attempts,
       available_at, lease_owner, lease_until, heartbeat_at, last_error_code,
       created_at, updated_at, delivered_at
FROM companion_nexus_outbox;
