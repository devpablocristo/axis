SET lock_timeout = '5s';
SET statement_timeout = '30s';

ALTER TABLE job_roles
    DROP CONSTRAINT IF EXISTS job_roles_responsibilities_array_check,
    DROP CONSTRAINT IF EXISTS job_roles_success_criteria_array_check,
    DROP COLUMN IF EXISTS responsibilities_json,
    DROP COLUMN IF EXISTS success_criteria_json;
