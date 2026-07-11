-- platform:migrate:non-transactional
SET lock_timeout = '5s';
SET statement_timeout = '30s';

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_job_roles_lifecycle
    ON job_roles (tenant_id, archived_at, trashed_at);

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_job_roles_tenant_id
    ON job_roles (tenant_id, id);

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_virployees_job_role_id
    ON virployees (tenant_id, job_role_id);

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_profile_templates_lifecycle
    ON profile_templates (tenant_id, archived_at, trashed_at);

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_profile_templates_tenant_id
    ON profile_templates (tenant_id, id);

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_virployees_profile_template_id
    ON virployees (tenant_id, profile_template_id);
