SET lock_timeout = '5s';
SET statement_timeout = '30s';

CREATE TABLE IF NOT EXISTS functional_role_grants (
    id uuid PRIMARY KEY,
    tenant_id text NOT NULL,
    user_id text NOT NULL,
    role_key text NOT NULL CHECK (role_key IN ('policy_admin','approver','auditor','delegation_admin')),
    product_surface text NOT NULL DEFAULT '',
    action_type_pattern text NOT NULL DEFAULT '*',
    resource_type text NOT NULL DEFAULT '',
    resource_id text NOT NULL DEFAULT '',
    max_risk_class text NOT NULL DEFAULT 'critical' CHECK (max_risk_class IN ('low','medium','high','critical')),
    valid_from timestamptz NOT NULL,
    valid_until timestamptz NOT NULL,
    revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
    granted_by text NOT NULL,
    revoked_at timestamptz NULL,
    revoked_by text NOT NULL DEFAULT '',
    revocation_reason text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CHECK (valid_until > valid_from)
);

CREATE INDEX IF NOT EXISTS functional_role_grants_lookup_idx
    ON functional_role_grants (tenant_id,user_id,role_key,valid_from,valid_until)
    WHERE revoked_at IS NULL;

CREATE TABLE IF NOT EXISTS functional_role_grant_audit (
    id uuid PRIMARY KEY,
    tenant_id text NOT NULL,
    grant_id uuid NOT NULL,
    actor_id text NOT NULL,
    action text NOT NULL CHECK (action IN ('granted','revoked')),
    revision bigint NOT NULL,
    snapshot jsonb NOT NULL,
    created_at timestamptz NOT NULL
);

CREATE INDEX IF NOT EXISTS functional_role_grant_audit_tenant_idx
    ON functional_role_grant_audit (tenant_id,created_at DESC,id DESC);

CREATE TABLE IF NOT EXISTS governance_policy_artifacts (
    id uuid PRIMARY KEY,
    tenant_id text NOT NULL,
    policy_key text NOT NULL,
    name text NOT NULL,
    description text NOT NULL DEFAULT '',
    created_by text NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    UNIQUE (tenant_id,policy_key)
);

CREATE TABLE IF NOT EXISTS governance_policy_versions (
    id uuid PRIMARY KEY,
    tenant_id text NOT NULL,
    policy_id uuid NOT NULL REFERENCES governance_policy_artifacts(id),
    version integer NOT NULL CHECK (version > 0),
    state text NOT NULL CHECK (state IN ('draft','shadow','active','retired')),
    product_surface text NOT NULL DEFAULT '',
    action_type_pattern text NOT NULL DEFAULT '*',
    target_system text NOT NULL DEFAULT '',
    requester_type text NOT NULL DEFAULT '',
    expression text NOT NULL DEFAULT '',
    effect text NOT NULL CHECK (effect IN ('allow','deny','require_approval')),
    risk_override text NULL CHECK (risk_override IS NULL OR risk_override IN ('low','medium','high','critical')),
    priority integer NOT NULL DEFAULT 100,
    content_hash text NOT NULL,
    created_by text NOT NULL,
    created_at timestamptz NOT NULL,
    retired_at timestamptz NULL,
    UNIQUE (tenant_id,policy_id,version)
);

CREATE INDEX IF NOT EXISTS governance_policy_versions_active_idx
    ON governance_policy_versions (tenant_id,state,priority,created_at)
    WHERE state IN ('shadow','active');

CREATE TABLE IF NOT EXISTS governance_policy_simulations (
    id uuid PRIMARY KEY,
    tenant_id text NOT NULL,
    policy_version_id uuid NOT NULL REFERENCES governance_policy_versions(id),
    requested_by text NOT NULL,
    total_evaluated integer NOT NULL,
    would_match integer NOT NULL,
    would_allow integer NOT NULL,
    would_deny integer NOT NULL,
    would_require_approval integer NOT NULL,
    report_hash text NOT NULL,
    created_at timestamptz NOT NULL
);

CREATE TABLE IF NOT EXISTS governance_policy_promotions (
    id uuid PRIMARY KEY,
    tenant_id text NOT NULL,
    policy_version_id uuid NOT NULL REFERENCES governance_policy_versions(id),
    simulation_id uuid NOT NULL REFERENCES governance_policy_simulations(id),
    target_state text NOT NULL CHECK (target_state IN ('shadow','active')),
    status text NOT NULL CHECK (status IN ('pending','approved','rejected')),
    requested_by text NOT NULL,
    decided_by text NOT NULL DEFAULT '',
    decision_reason text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL,
    decided_at timestamptz NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS governance_policy_promotions_pending_idx
    ON governance_policy_promotions (tenant_id,policy_version_id,target_state)
    WHERE status='pending';

CREATE TABLE IF NOT EXISTS governance_policy_evaluations (
    id uuid PRIMARY KEY,
    tenant_id text NOT NULL,
    governance_check_id uuid NULL,
    policy_version_id uuid NOT NULL REFERENCES governance_policy_versions(id),
    mode text NOT NULL CHECK (mode IN ('shadow','enforced')),
    matched boolean NOT NULL,
    effect text NOT NULL,
    decision text NOT NULL DEFAULT '',
    error_code text NOT NULL DEFAULT '',
    input_hash text NOT NULL,
    created_at timestamptz NOT NULL
);

CREATE INDEX IF NOT EXISTS governance_policy_evaluations_tenant_idx
    ON governance_policy_evaluations (tenant_id,created_at DESC,id DESC);

CREATE TABLE IF NOT EXISTS governance_policy_changelog (
    id uuid PRIMARY KEY,
    tenant_id text NOT NULL,
    policy_id uuid NOT NULL REFERENCES governance_policy_artifacts(id),
    policy_version_id uuid NULL REFERENCES governance_policy_versions(id),
    actor_id text NOT NULL,
    action text NOT NULL,
    summary text NOT NULL,
    data jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL
);

ALTER TABLE governance_checks ADD COLUMN IF NOT EXISTS policy_snapshot_hash text NOT NULL DEFAULT '';
ALTER TABLE governance_checks ADD COLUMN IF NOT EXISTS policy_matches jsonb NOT NULL DEFAULT '[]'::jsonb;
ALTER TABLE governance_checks ADD COLUMN IF NOT EXISTS policy_input_hash text NOT NULL DEFAULT '';
ALTER TABLE governance_checks ADD COLUMN IF NOT EXISTS product_surface text NOT NULL DEFAULT '';
ALTER TABLE governance_checks ADD COLUMN IF NOT EXISTS requester_type text NOT NULL DEFAULT '';
ALTER TABLE governance_checks ADD COLUMN IF NOT EXISTS resource_type text NOT NULL DEFAULT '';
ALTER TABLE governance_checks ADD COLUMN IF NOT EXISTS membership_role text NOT NULL DEFAULT '';
ALTER TABLE governance_checks ADD COLUMN IF NOT EXISTS functional_roles jsonb NOT NULL DEFAULT '[]'::jsonb;
ALTER TABLE governance_checks ADD COLUMN IF NOT EXISTS functional_scopes jsonb NOT NULL DEFAULT '[]'::jsonb;
ALTER TABLE approvals ADD COLUMN IF NOT EXISTS governance_policy_snapshot_hash text NOT NULL DEFAULT '';

COMMENT ON TABLE governance_policy_evaluations IS
    'Metadata-only policy evaluation audit. CEL input bodies and sensitive request content are never stored.';
