DROP TABLE IF EXISTS nexus_reconciliation_findings;
DROP TABLE IF EXISTS nexus_reconciliation_runs;
DROP TABLE IF EXISTS nexus_rate_limit_decisions;
DROP TABLE IF EXISTS nexus_rate_limit_counters;
DROP TABLE IF EXISTS governance_export_jobs;
DROP TABLE IF EXISTS governance_retention_policies;
DROP TABLE IF EXISTS governance_legal_holds;
DROP TABLE IF EXISTS policy_simulation_samples;
DROP TABLE IF EXISTS policy_simulation_runs;
DROP TABLE IF EXISTS policy_freeze_windows;
DROP TABLE IF EXISTS policy_promotion_requests;
DROP TABLE IF EXISTS nexus_callback_delivery_events;

ALTER TABLE nexus_callback_deliveries
    DROP COLUMN IF EXISTS poison_reason,
    DROP COLUMN IF EXISTS replayed_from_delivery_id,
    DROP COLUMN IF EXISTS last_attempt_at,
    DROP COLUMN IF EXISTS leased_until,
    DROP COLUMN IF EXISTS lease_owner;
