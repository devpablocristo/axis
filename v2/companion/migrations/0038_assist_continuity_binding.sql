SET lock_timeout = '5s';
SET statement_timeout = '30s';

ALTER TABLE companion_assist_runs
    ADD COLUMN IF NOT EXISTS continuity_assignment_id uuid NULL,
    ADD COLUMN IF NOT EXISTS continuity_assignment_version bigint NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS context_hash text NOT NULL DEFAULT '';

ALTER TABLE companion_assist_runs
    ADD CONSTRAINT companion_assist_runs_continuity_assignment_fkey
    FOREIGN KEY (tenant_id, continuity_assignment_id)
    REFERENCES companion_continuity_assignments (tenant_id, id) NOT VALID;

ALTER TABLE companion_assist_runs
    VALIDATE CONSTRAINT companion_assist_runs_continuity_assignment_fkey;

ALTER TABLE companion_assist_runs
    ADD CONSTRAINT companion_assist_runs_continuity_version_check
    CHECK (
        (continuity_assignment_id IS NULL AND continuity_assignment_version = 0)
        OR (continuity_assignment_id IS NOT NULL AND continuity_assignment_version > 0)
    ) NOT VALID;

ALTER TABLE companion_assist_runs
    VALIDATE CONSTRAINT companion_assist_runs_continuity_version_check;

CREATE INDEX IF NOT EXISTS idx_companion_assist_runs_continuity_assignment
    ON companion_assist_runs (tenant_id, continuity_assignment_id, started_at DESC)
    WHERE continuity_assignment_id IS NOT NULL;
