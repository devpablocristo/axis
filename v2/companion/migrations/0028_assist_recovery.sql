ALTER TABLE companion_assist_runs
    ADD COLUMN IF NOT EXISTS recovery_attempts integer NOT NULL DEFAULT 0 CHECK (recovery_attempts >= 0);

CREATE INDEX IF NOT EXISTS companion_assist_runs_recovery_idx
    ON companion_assist_runs (updated_at, id)
    WHERE status IN ('staging','extracting','indexing','answering');
