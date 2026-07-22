SET lock_timeout = '5s';
SET statement_timeout = '30s';

ALTER TABLE companion_assist_runs
    ADD COLUMN IF NOT EXISTS input_json jsonb NOT NULL DEFAULT '{}'::jsonb;

ALTER TABLE companion_assist_runs
    DROP CONSTRAINT IF EXISTS companion_assist_runs_status_check;

UPDATE companion_assist_runs SET status = 'answering' WHERE status = 'running';

ALTER TABLE companion_assist_runs
    ADD CONSTRAINT companion_assist_runs_status_check CHECK (
        status IN ('received', 'answering', 'done', 'failed')
    ) NOT VALID;
ALTER TABLE companion_assist_runs VALIDATE CONSTRAINT companion_assist_runs_status_check;

DROP INDEX IF EXISTS idx_companion_assist_runs_stale;
CREATE INDEX idx_companion_assist_runs_stale
    ON companion_assist_runs (updated_at, id) WHERE status = 'answering';

CREATE INDEX idx_companion_assist_runs_received
    ON companion_assist_runs (updated_at, id) WHERE status = 'received';
