CREATE TABLE IF NOT EXISTS approvals (
    id uuid PRIMARY KEY,
    tenant_id text NOT NULL DEFAULT 'default',
    governance_check_id uuid NOT NULL REFERENCES governance_checks(id) ON DELETE CASCADE,
    requester_id text NOT NULL,
    action_type text NOT NULL,
    target_system text NOT NULL DEFAULT '',
    target_resource text NOT NULL DEFAULT '',
    risk_level text NOT NULL DEFAULT '',
    reason text NOT NULL DEFAULT '',
    binding_hash text NOT NULL DEFAULT '',
    status text NOT NULL DEFAULT 'pending',
    decided_by text NOT NULL DEFAULT '',
    decision_note text NOT NULL DEFAULT '',
    decided_at timestamptz NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CONSTRAINT approvals_status_check CHECK (
        status IN ('pending', 'approved', 'rejected')
    )
);

CREATE INDEX IF NOT EXISTS idx_approvals_tenant_status_created
    ON approvals (tenant_id, status, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_approvals_binding_hash
    ON approvals (tenant_id, binding_hash)
    WHERE binding_hash <> '';
