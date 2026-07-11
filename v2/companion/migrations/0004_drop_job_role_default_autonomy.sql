SET lock_timeout = '5s';
SET statement_timeout = '30s';

ALTER TABLE job_roles
    DROP CONSTRAINT IF EXISTS job_roles_autonomy_check;

ALTER TABLE job_roles
    DROP COLUMN IF EXISTS default_autonomy;
