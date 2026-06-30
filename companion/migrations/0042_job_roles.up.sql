CREATE TABLE IF NOT EXISTS companion_job_roles (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    job_role_id text NOT NULL,
    org_id text NOT NULL,
    product_surface text NOT NULL,
    name text NOT NULL,
    slug text NOT NULL,
    description text NOT NULL DEFAULT '',
    mission text NOT NULL DEFAULT '',
    responsibilities_json jsonb NOT NULL DEFAULT '[]'::jsonb,
    recommended_capabilities text[] NOT NULL DEFAULT '{}',
    default_autonomy_level text NOT NULL DEFAULT 'A2',
    default_permission_bundle_id text NOT NULL DEFAULT '',
    success_criteria text[] NOT NULL DEFAULT '{}',
    default_sla_policy_json jsonb NOT NULL DEFAULT '{}'::jsonb,
    default_memory_policy_json jsonb NOT NULL DEFAULT '{}'::jsonb,
    status text NOT NULL DEFAULT 'active',
    metadata_json jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_by text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    archived_at timestamptz,
    version bigint NOT NULL DEFAULT 1,
    CONSTRAINT companion_job_roles_status_check CHECK (status IN ('active', 'archived')),
    CONSTRAINT companion_job_roles_autonomy_check CHECK (default_autonomy_level IN ('A0', 'A1', 'A2', 'A3', 'A4', 'A5')),
    CONSTRAINT companion_job_roles_responsibilities_array_check CHECK (jsonb_typeof(responsibilities_json) = 'array'),
    CONSTRAINT companion_job_roles_sla_object_check CHECK (jsonb_typeof(default_sla_policy_json) = 'object'),
    CONSTRAINT companion_job_roles_memory_object_check CHECK (jsonb_typeof(default_memory_policy_json) = 'object'),
    CONSTRAINT companion_job_roles_metadata_object_check CHECK (jsonb_typeof(metadata_json) = 'object'),
    UNIQUE (org_id, product_surface, job_role_id),
    UNIQUE (org_id, product_surface, slug)
);

CREATE INDEX IF NOT EXISTS companion_job_roles_tenant_status_idx
    ON companion_job_roles (org_id, product_surface, status, name);

CREATE TABLE IF NOT EXISTS companion_job_role_audit (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    job_role_id text NOT NULL,
    org_id text NOT NULL,
    product_surface text NOT NULL,
    version bigint NOT NULL,
    action text NOT NULL,
    changed_by text NOT NULL DEFAULT '',
    role_json jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT companion_job_role_audit_role_object_check CHECK (jsonb_typeof(role_json) = 'object')
);

CREATE INDEX IF NOT EXISTS companion_job_role_audit_role_idx
    ON companion_job_role_audit (org_id, product_surface, job_role_id, version DESC);
