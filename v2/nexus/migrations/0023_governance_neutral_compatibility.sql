SET lock_timeout = '5s';
SET statement_timeout = '30s';

-- Active v2 snapshots and historical observations may still use
-- via_companion. New observations use via_orchestrator; both remain readable
-- until all v2 installations have been retired.
ALTER TABLE nexus_product_service_activity
    DROP CONSTRAINT IF EXISTS nexus_product_service_activity_access_mode_check;

ALTER TABLE nexus_product_service_activity
    ADD CONSTRAINT nexus_product_service_activity_access_mode_check
    CHECK (access_mode IN ('direct', 'via_orchestrator', 'via_companion'))
    NOT VALID;

ALTER TABLE nexus_product_service_activity
    VALIDATE CONSTRAINT nexus_product_service_activity_access_mode_check;

-- Preserve historical rows for audit and foreign-key references, but stop
-- advertising domain-specific executors from the generic Axis catalog.
UPDATE action_types
SET enabled = false,
    updated_at = now()
WHERE org_id = 'default'
  AND id IN (
      '00000000-0000-0000-0000-000000000101',
      '00000000-0000-0000-0000-000000000102',
      '00000000-0000-0000-0000-000000000103',
      '00000000-0000-0000-0000-000000000104'
  );
