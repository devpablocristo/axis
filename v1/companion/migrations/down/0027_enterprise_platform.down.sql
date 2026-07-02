DROP INDEX IF EXISTS idx_security_eval_reports_org_time;
DROP INDEX IF EXISTS idx_security_eval_reports_suite_time;
DROP TABLE IF EXISTS companion_security_eval_reports;

DROP INDEX IF EXISTS idx_companion_cost_events_agent;
DROP INDEX IF EXISTS idx_companion_cost_events_run;
DROP INDEX IF EXISTS idx_companion_cost_events_org_time;
DROP TABLE IF EXISTS companion_cost_events;

DROP INDEX IF EXISTS idx_capability_conformance_runs_org;
DROP INDEX IF EXISTS idx_capability_conformance_runs_capability;
DROP TABLE IF EXISTS companion_capability_conformance_runs;

DROP INDEX IF EXISTS idx_capability_manifests_status;
DROP TABLE IF EXISTS companion_capability_manifests;

DROP INDEX IF EXISTS idx_memory_reviews_memory;
DROP INDEX IF EXISTS idx_memory_reviews_org_status;
DROP TABLE IF EXISTS companion_memory_reviews;

DROP INDEX IF EXISTS idx_memory_vectors_org_surface;
DROP INDEX IF EXISTS idx_memory_vectors_namespace;
DROP TABLE IF EXISTS companion_memory_vectors;
