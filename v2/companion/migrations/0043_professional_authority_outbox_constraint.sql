SET lock_timeout = '5s';
SET statement_timeout = '30s';

-- Some installations applied migration 0037 before professional-authority
-- events were added to its final form. Repair the legacy single-value checks
-- in a forward-only migration so both fresh and already-migrated databases
-- accept the paired aggregate/kind contract.
ALTER TABLE companion_nexus_outbox
    DROP CONSTRAINT IF EXISTS companion_nexus_outbox_aggregate_type_check;
ALTER TABLE companion_nexus_outbox
    DROP CONSTRAINT IF EXISTS companion_nexus_outbox_kind_check;
ALTER TABLE companion_nexus_outbox
    DROP CONSTRAINT IF EXISTS companion_nexus_outbox_type_kind_check;
ALTER TABLE companion_nexus_outbox
    ADD CONSTRAINT companion_nexus_outbox_type_kind_check CHECK (
        (aggregate_type = 'execution_attempt' AND kind = 'execution_result') OR
        (aggregate_type = 'professional_authority' AND kind = 'audit_event')
    );
