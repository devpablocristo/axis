DROP INDEX IF EXISTS idx_companion_job_events_job_created;

DROP TABLE IF EXISTS companion_job_events;

DROP INDEX IF EXISTS idx_companion_jobs_lease;
DROP INDEX IF EXISTS idx_companion_jobs_org_kind;
DROP INDEX IF EXISTS idx_companion_jobs_claim;
DROP INDEX IF EXISTS idx_companion_jobs_active_dedupe;

DROP TABLE IF EXISTS companion_jobs;
