DROP TABLE IF EXISTS nexus_operation_events;
DROP TABLE IF EXISTS nexus_rate_limit_rules;
DROP TABLE IF EXISTS break_glass_reviews;
DROP TABLE IF EXISTS approval_plans;
DROP TABLE IF EXISTS policy_promotions;
DROP TABLE IF EXISTS policy_changelog;
ALTER TABLE IF EXISTS policy_artifacts DROP CONSTRAINT IF EXISTS fk_policy_artifacts_current_version;
DROP TABLE IF EXISTS policy_versions;
DROP TABLE IF EXISTS policy_artifacts;
DROP TABLE IF EXISTS nexus_callback_deliveries;
DROP TABLE IF EXISTS nexus_outbox_events;
DROP TABLE IF EXISTS audit_integrity_checks;
DROP TABLE IF EXISTS governance_contract_validation_reports;
DROP TABLE IF EXISTS governance_contracts;

DROP INDEX IF EXISTS idx_request_events_event_hash;
DROP INDEX IF EXISTS idx_request_events_chain_scope_created;

ALTER TABLE request_events
    DROP COLUMN IF EXISTS signature,
    DROP COLUMN IF EXISTS signature_key_id,
    DROP COLUMN IF EXISTS event_hash,
    DROP COLUMN IF EXISTS payload_hash,
    DROP COLUMN IF EXISTS previous_hash,
    DROP COLUMN IF EXISTS chain_scope;
