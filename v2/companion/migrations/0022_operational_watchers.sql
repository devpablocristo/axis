SET lock_timeout = '5s';
SET statement_timeout = '30s';

ALTER TABLE companion_assist_runs
    ADD COLUMN IF NOT EXISTS updated_at timestamptz;
UPDATE companion_assist_runs SET updated_at = COALESCE(completed_at, started_at) WHERE updated_at IS NULL;
ALTER TABLE companion_assist_runs ALTER COLUMN updated_at SET DEFAULT now();
ALTER TABLE companion_assist_runs ALTER COLUMN updated_at SET NOT NULL;

ALTER TABLE companion_execution_attempts
    ADD COLUMN IF NOT EXISTS recovery_attempts integer NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS recovery_lease_owner text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS recovery_lease_until timestamptz NULL,
    ADD COLUMN IF NOT EXISTS nexus_report_attempts integer NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS nexus_report_next_at timestamptz NOT NULL DEFAULT now(),
    ADD COLUMN IF NOT EXISTS report_lease_owner text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS report_lease_until timestamptz NULL,
    ADD COLUMN IF NOT EXISTS last_watcher_error text NOT NULL DEFAULT '';

ALTER TABLE companion_execution_attempts
    DROP CONSTRAINT IF EXISTS companion_execution_attempts_report_status_check;
ALTER TABLE companion_execution_attempts
    ADD CONSTRAINT companion_execution_attempts_report_status_check CHECK (
        nexus_report_status IN ('pending', 'reported', 'failed', 'dead')
    ) NOT VALID;
ALTER TABLE companion_execution_attempts
    VALIDATE CONSTRAINT companion_execution_attempts_report_status_check;

CREATE INDEX IF NOT EXISTS idx_companion_assist_runs_stale
    ON companion_assist_runs (updated_at, id) WHERE status = 'running';
CREATE INDEX IF NOT EXISTS idx_companion_execution_attempts_stale
    ON companion_execution_attempts (updated_at, id) WHERE status = 'running';
CREATE INDEX IF NOT EXISTS idx_companion_execution_reports_due
    ON companion_execution_attempts (nexus_report_next_at, id)
    WHERE status IN ('succeeded', 'failed') AND nexus_report_status IN ('pending', 'failed');
