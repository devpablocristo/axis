ALTER TABLE companion_job_role_audit
    DROP COLUMN IF EXISTS tenant_id;

DROP INDEX IF EXISTS companion_job_roles_tenant_id_status_idx;
DROP INDEX IF EXISTS companion_job_roles_tenant_slug_uidx;

ALTER TABLE companion_job_roles
    DROP CONSTRAINT IF EXISTS companion_job_roles_success_criteria_array_check,
    DROP COLUMN IF EXISTS success_criteria_json,
    DROP COLUMN IF EXISTS recommended_capability_ids,
    DROP COLUMN IF EXISTS tenant_id;
