ALTER TABLE companion_job_roles
    ADD COLUMN IF NOT EXISTS tenant_id text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS recommended_capability_ids uuid[] NOT NULL DEFAULT '{}',
    ADD COLUMN IF NOT EXISTS success_criteria_json jsonb NOT NULL DEFAULT '[]'::jsonb;

ALTER TABLE companion_job_roles
    ADD CONSTRAINT companion_job_roles_success_criteria_array_check
        CHECK (jsonb_typeof(success_criteria_json) = 'array');

CREATE UNIQUE INDEX IF NOT EXISTS companion_job_roles_tenant_slug_uidx
    ON companion_job_roles (tenant_id, slug)
    WHERE tenant_id <> '';

CREATE INDEX IF NOT EXISTS companion_job_roles_tenant_id_status_idx
    ON companion_job_roles (tenant_id, status, name)
    WHERE tenant_id <> '';

ALTER TABLE companion_job_role_audit
    ADD COLUMN IF NOT EXISTS tenant_id text NOT NULL DEFAULT '';
