SET lock_timeout = '5s';
SET statement_timeout = '30s';

CREATE TABLE IF NOT EXISTS professional_scope_policies (
    tenant_id text NOT NULL CHECK (btrim(tenant_id) <> ''),
    virployee_id uuid NOT NULL,
    allowed_topics jsonb NOT NULL DEFAULT '[]'::jsonb,
    prohibited_topics jsonb NOT NULL DEFAULT '[]'::jsonb,
    out_of_scope text NOT NULL DEFAULT 'abstain'
        CHECK (out_of_scope IN ('abstain', 'escalate')),
    revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, virployee_id),
    CONSTRAINT professional_scope_policies_virployee_fkey
        FOREIGN KEY (tenant_id, virployee_id) REFERENCES virployees (tenant_id, id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS professional_policy_packs (
    id uuid PRIMARY KEY,
    tenant_id text NOT NULL CHECK (btrim(tenant_id) <> ''),
    policy_key text NOT NULL CHECK (policy_key ~ '^[a-z0-9][a-z0-9._-]{0,127}$'),
    name text NOT NULL CHECK (btrim(name) <> ''),
    version integer NOT NULL CHECK (version > 0),
    job_role_id uuid NULL,
    rules jsonb NOT NULL DEFAULT '{}'::jsonb,
    revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
    active boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, policy_key, version),
    UNIQUE (tenant_id, id),
    CONSTRAINT professional_policy_packs_job_role_fkey
        FOREIGN KEY (tenant_id, job_role_id) REFERENCES job_roles (tenant_id, id)
);

CREATE INDEX IF NOT EXISTS idx_professional_policy_packs_job_role
    ON professional_policy_packs (tenant_id, job_role_id, active)
    WHERE job_role_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS virployee_policy_bindings (
    tenant_id text NOT NULL CHECK (btrim(tenant_id) <> ''),
    virployee_id uuid NOT NULL,
    revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, virployee_id),
    CONSTRAINT virployee_policy_bindings_virployee_fkey
        FOREIGN KEY (tenant_id, virployee_id) REFERENCES virployees (tenant_id, id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS virployee_policy_pack_assignments (
    tenant_id text NOT NULL CHECK (btrim(tenant_id) <> ''),
    virployee_id uuid NOT NULL,
    policy_pack_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, virployee_id, policy_pack_id),
    CONSTRAINT virployee_policy_pack_assignments_virployee_fkey
        FOREIGN KEY (tenant_id, virployee_id) REFERENCES virployees (tenant_id, id) ON DELETE CASCADE,
    CONSTRAINT virployee_policy_pack_assignments_pack_fkey
        FOREIGN KEY (tenant_id, policy_pack_id) REFERENCES professional_policy_packs (tenant_id, id)
);

CREATE INDEX IF NOT EXISTS idx_virployee_policy_assignments_pack
    ON virployee_policy_pack_assignments (tenant_id, policy_pack_id);

CREATE TABLE IF NOT EXISTS professional_delegations (
    id uuid PRIMARY KEY,
    tenant_id text NOT NULL CHECK (btrim(tenant_id) <> ''),
    virployee_id uuid NOT NULL,
    principal_type text NOT NULL CHECK (principal_type IN ('person', 'organization', 'team', 'case', 'project', 'service')),
    principal_id text NOT NULL CHECK (btrim(principal_id) <> ''),
    capability_scopes jsonb NOT NULL,
    valid_from timestamptz NOT NULL,
    valid_until timestamptz NOT NULL,
    revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
    revoked_at timestamptz NULL,
    revoked_by text NOT NULL DEFAULT '',
    revocation_reason text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CHECK (valid_until > valid_from),
    CONSTRAINT professional_delegations_virployee_fkey
        FOREIGN KEY (tenant_id, virployee_id) REFERENCES virployees (tenant_id, id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_professional_delegations_resolution
    ON professional_delegations (tenant_id, virployee_id, valid_from, valid_until)
    WHERE revoked_at IS NULL;

-- Professional-authority audit events share the existing transactional Nexus
-- outbox and its lease/retry/dead-letter worker. The paired check prevents a
-- kind from being projected or delivered as the wrong aggregate type.
ALTER TABLE companion_nexus_outbox
    DROP CONSTRAINT IF EXISTS companion_nexus_outbox_aggregate_type_check;
ALTER TABLE companion_nexus_outbox
    DROP CONSTRAINT IF EXISTS companion_nexus_outbox_kind_check;
ALTER TABLE companion_nexus_outbox
    DROP CONSTRAINT IF EXISTS companion_nexus_outbox_type_kind_check;
ALTER TABLE companion_nexus_outbox
    ADD CONSTRAINT companion_nexus_outbox_type_kind_check CHECK (
        (aggregate_type = 'execution_attempt' AND kind = 'execution_result') OR
        (aggregate_type = 'professional_authority' AND kind = 'audit_event')
    );

ALTER TABLE companion_prepared_actions
    ADD COLUMN IF NOT EXISTS authority_binding_hash text NOT NULL DEFAULT '';
