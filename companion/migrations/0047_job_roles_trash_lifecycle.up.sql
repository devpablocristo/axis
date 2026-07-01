ALTER TABLE companion_job_roles
    DROP CONSTRAINT IF EXISTS companion_job_roles_status_check;

ALTER TABLE companion_job_roles
    ADD CONSTRAINT companion_job_roles_status_check
        CHECK (status IN ('active', 'archived', 'trash'));
