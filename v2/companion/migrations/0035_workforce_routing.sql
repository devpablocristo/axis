SET lock_timeout = '5s';
SET statement_timeout = '30s';

ALTER TABLE job_roles
    ADD COLUMN IF NOT EXISTS responsibilities_json jsonb NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN IF NOT EXISTS success_criteria_json jsonb NOT NULL DEFAULT '[]'::jsonb;

ALTER TABLE job_roles
    DROP CONSTRAINT IF EXISTS job_roles_responsibilities_array_check,
    DROP CONSTRAINT IF EXISTS job_roles_success_criteria_array_check;

ALTER TABLE job_roles
    ADD CONSTRAINT job_roles_responsibilities_array_check
        CHECK (jsonb_typeof(responsibilities_json) = 'array'),
    ADD CONSTRAINT job_roles_success_criteria_array_check
        CHECK (jsonb_typeof(success_criteria_json) = 'array');

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'job_roles_tenant_id_unique') THEN
        ALTER TABLE job_roles ADD CONSTRAINT job_roles_tenant_id_unique UNIQUE (tenant_id, id);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'virployees_tenant_id_unique') THEN
        ALTER TABLE virployees ADD CONSTRAINT virployees_tenant_id_unique UNIQUE (tenant_id, id);
    END IF;
END $$;

CREATE TABLE IF NOT EXISTS companion_work_subjects (
    id uuid PRIMARY KEY,
    tenant_id text NOT NULL,
    kind text NOT NULL,
    display_name text NOT NULL,
    external_ref text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    archived_at timestamptz NULL,
    CONSTRAINT companion_work_subjects_kind_check
        CHECK (kind IN ('person', 'organization', 'team', 'patient')),
    CONSTRAINT companion_work_subjects_display_name_check
        CHECK (length(btrim(display_name)) > 0),
    CONSTRAINT companion_work_subjects_tenant_id_unique UNIQUE (tenant_id, id)
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_companion_work_subjects_external_ref
    ON companion_work_subjects (tenant_id, external_ref)
    WHERE external_ref <> '';
CREATE INDEX IF NOT EXISTS idx_companion_work_subjects_active
    ON companion_work_subjects (tenant_id, kind, display_name, id)
    WHERE archived_at IS NULL;

CREATE TABLE IF NOT EXISTS companion_routing_pools (
    id uuid PRIMARY KEY,
    tenant_id text NOT NULL,
    job_role_id uuid NOT NULL,
    name text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    archived_at timestamptz NULL,
    CONSTRAINT companion_routing_pools_name_check CHECK (length(btrim(name)) > 0),
    CONSTRAINT companion_routing_pools_tenant_name_unique UNIQUE (tenant_id, name),
    CONSTRAINT companion_routing_pools_tenant_id_unique UNIQUE (tenant_id, id),
    CONSTRAINT companion_routing_pools_job_role_fkey
        FOREIGN KEY (tenant_id, job_role_id) REFERENCES job_roles (tenant_id, id)
);

CREATE INDEX IF NOT EXISTS idx_companion_routing_pools_active
    ON companion_routing_pools (tenant_id, job_role_id, name, id)
    WHERE archived_at IS NULL;

CREATE TABLE IF NOT EXISTS companion_routing_pool_members (
    tenant_id text NOT NULL,
    pool_id uuid NOT NULL,
    virployee_id uuid NOT NULL,
    max_active_subjects integer NOT NULL,
    enabled boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, pool_id, virployee_id),
    CONSTRAINT companion_routing_pool_members_capacity_check CHECK (max_active_subjects > 0),
    CONSTRAINT companion_routing_pool_members_pool_fkey
        FOREIGN KEY (tenant_id, pool_id)
        REFERENCES companion_routing_pools (tenant_id, id) ON DELETE CASCADE,
    CONSTRAINT companion_routing_pool_members_virployee_fkey
        FOREIGN KEY (tenant_id, virployee_id)
        REFERENCES virployees (tenant_id, id)
);

CREATE TABLE IF NOT EXISTS companion_virployee_relationships (
    id uuid PRIMARY KEY,
    tenant_id text NOT NULL,
    virployee_id uuid NOT NULL,
    subject_id uuid NOT NULL,
    relationship_type text NOT NULL,
    is_primary boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT companion_virployee_relationships_type_check
        CHECK (relationship_type IN ('works_for', 'serves', 'reports_to')),
    CONSTRAINT companion_virployee_relationships_primary_check
        CHECK (NOT is_primary OR relationship_type = 'works_for'),
    CONSTRAINT companion_virployee_relationships_identity_unique
        UNIQUE (tenant_id, virployee_id, relationship_type, subject_id),
    CONSTRAINT companion_virployee_relationships_virployee_fkey
        FOREIGN KEY (tenant_id, virployee_id) REFERENCES virployees (tenant_id, id) ON DELETE CASCADE,
    CONSTRAINT companion_virployee_relationships_subject_fkey
        FOREIGN KEY (tenant_id, subject_id) REFERENCES companion_work_subjects (tenant_id, id)
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_companion_virployee_relationships_primary_employer
    ON companion_virployee_relationships (tenant_id, virployee_id)
    WHERE relationship_type = 'works_for' AND is_primary;

CREATE TABLE IF NOT EXISTS companion_continuity_assignments (
    id uuid PRIMARY KEY,
    tenant_id text NOT NULL,
    pool_id uuid NOT NULL,
    subject_id uuid NOT NULL,
    virployee_id uuid NOT NULL,
    status text NOT NULL DEFAULT 'active',
    version bigint NOT NULL DEFAULT 1,
    assigned_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT companion_continuity_assignments_status_check CHECK (status = 'active'),
    CONSTRAINT companion_continuity_assignments_scope_unique UNIQUE (tenant_id, pool_id, subject_id),
    CONSTRAINT companion_continuity_assignments_tenant_id_unique UNIQUE (tenant_id, id),
    CONSTRAINT companion_continuity_assignments_pool_fkey
        FOREIGN KEY (tenant_id, pool_id) REFERENCES companion_routing_pools (tenant_id, id),
    CONSTRAINT companion_continuity_assignments_subject_fkey
        FOREIGN KEY (tenant_id, subject_id) REFERENCES companion_work_subjects (tenant_id, id),
    CONSTRAINT companion_continuity_assignments_member_fkey
        FOREIGN KEY (tenant_id, pool_id, virployee_id)
        REFERENCES companion_routing_pool_members (tenant_id, pool_id, virployee_id)
);

CREATE INDEX IF NOT EXISTS idx_companion_continuity_assignments_member
    ON companion_continuity_assignments (tenant_id, pool_id, virployee_id);

CREATE TABLE IF NOT EXISTS companion_continuity_assignment_events (
    id uuid PRIMARY KEY,
    tenant_id text NOT NULL,
    assignment_id uuid NOT NULL,
    event_type text NOT NULL,
    previous_virployee_id uuid NULL,
    virployee_id uuid NOT NULL,
    actor_id text NOT NULL,
    reason_code text NOT NULL,
    assignment_version bigint NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT companion_continuity_assignment_events_type_check
        CHECK (event_type IN ('assigned', 'reassigned')),
    CONSTRAINT companion_continuity_assignment_events_reason_check
        CHECK (reason_code ~ '^[a-z][a-z0-9_.-]{0,63}$'),
    CONSTRAINT companion_continuity_assignment_events_assignment_fkey
        FOREIGN KEY (tenant_id, assignment_id)
        REFERENCES companion_continuity_assignments (tenant_id, id) ON DELETE CASCADE,
    CONSTRAINT companion_continuity_assignment_events_previous_virployee_fkey
        FOREIGN KEY (tenant_id, previous_virployee_id)
        REFERENCES virployees (tenant_id, id),
    CONSTRAINT companion_continuity_assignment_events_virployee_fkey
        FOREIGN KEY (tenant_id, virployee_id)
        REFERENCES virployees (tenant_id, id)
);

CREATE INDEX IF NOT EXISTS idx_companion_continuity_assignment_events_assignment
    ON companion_continuity_assignment_events (tenant_id, assignment_id, created_at DESC);
