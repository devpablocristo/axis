ALTER TABLE companion_assist_runs
    ADD COLUMN IF NOT EXISTS product_surface text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS subject_id text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS repository_generation text NOT NULL DEFAULT '';

ALTER TABLE companion_assist_runs DROP CONSTRAINT IF EXISTS companion_assist_runs_status_check;
ALTER TABLE companion_assist_runs
    ADD CONSTRAINT companion_assist_runs_status_check
    CHECK (status IN ('received','staging','extracting','indexing','answering','done','failed'));
