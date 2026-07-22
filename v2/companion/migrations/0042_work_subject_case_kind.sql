SET lock_timeout = '5s';
SET statement_timeout = '30s';

ALTER TABLE companion_work_subjects
    DROP CONSTRAINT IF EXISTS companion_work_subjects_kind_check;
ALTER TABLE companion_work_subjects
    ADD CONSTRAINT companion_work_subjects_kind_check
    CHECK (kind IN ('person', 'organization', 'team', 'patient', 'case')) NOT VALID;
ALTER TABLE companion_work_subjects
    VALIDATE CONSTRAINT companion_work_subjects_kind_check;

-- A profession has one active routing authority per tenant. Without this
-- invariant, the same subject + Job Role could receive independent stable
-- assignments from parallel pools.
CREATE UNIQUE INDEX IF NOT EXISTS companion_routing_pools_active_job_role_uq
    ON companion_routing_pools (tenant_id, job_role_id)
    WHERE archived_at IS NULL;
