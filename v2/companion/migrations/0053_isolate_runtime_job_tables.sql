SET lock_timeout = '5s';
SET statement_timeout = '30s';

-- V1 owns companion_jobs and companion_job_events in shared databases. Their
-- payload/error columns differ from v2, so never reuse or mutate them. Preserve
-- existing v2 installations by renaming only the tables with the v2 shape.
DO $$
BEGIN
    IF to_regclass('companion_runtime_jobs') IS NULL
       AND EXISTS (
           SELECT 1
           FROM information_schema.columns
           WHERE table_schema = current_schema()
             AND table_name = 'companion_jobs'
             AND column_name = 'last_error_code'
       ) THEN
        ALTER TABLE companion_jobs RENAME TO companion_runtime_jobs;
    END IF;

    IF to_regclass('companion_runtime_job_events') IS NULL
       AND EXISTS (
           SELECT 1
           FROM information_schema.columns
           WHERE table_schema = current_schema()
             AND table_name = 'companion_job_events'
             AND column_name = 'metadata_json'
       ) THEN
        ALTER TABLE companion_job_events RENAME TO companion_runtime_job_events;
    END IF;
END $$;
