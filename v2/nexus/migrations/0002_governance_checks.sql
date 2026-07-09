CREATE TABLE IF NOT EXISTS governance_checks (
    id uuid PRIMARY KEY,
    tenant_id text NOT NULL DEFAULT 'default',
    requester_id text NOT NULL,
    action_type text NOT NULL,
    target_system text NOT NULL DEFAULT '',
    target_resource text NOT NULL DEFAULT '',
    decision text NOT NULL,
    risk_level text NOT NULL DEFAULT '',
    status text NOT NULL,
    decision_reason text NOT NULL DEFAULT '',
    binding_hash text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_governance_checks_tenant_created
    ON governance_checks (tenant_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_governance_checks_binding_hash
    ON governance_checks (tenant_id, binding_hash)
    WHERE binding_hash <> '';
