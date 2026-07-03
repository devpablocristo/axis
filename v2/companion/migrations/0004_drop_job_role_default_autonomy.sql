ALTER TABLE job_roles
    DROP CONSTRAINT IF EXISTS job_roles_autonomy_check;

ALTER TABLE job_roles
    DROP COLUMN IF EXISTS default_autonomy;
