-- Enterprise governance hardening foundations.
-- Additive only: existing v1 APIs and decision semantics remain compatible.

-- Versioned governance contracts (JSON-schema-like deterministic contracts).
CREATE TABLE IF NOT EXISTS governance_contracts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id TEXT,
    name TEXT NOT NULL,
    version TEXT NOT NULL,
    subject_type TEXT NOT NULL DEFAULT 'json',
    schema_json JSONB NOT NULL,
    status TEXT NOT NULL DEFAULT 'draft'
        CHECK (status IN ('draft', 'active', 'deprecated', 'archived')),
    validation_mode TEXT NOT NULL DEFAULT 'report_only'
        CHECK (validation_mode IN ('report_only', 'enforce')),
    compatibility TEXT NOT NULL DEFAULT 'backward',
    created_by TEXT NOT NULL DEFAULT 'system',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    promoted_at TIMESTAMPTZ,
    deprecated_at TIMESTAMPTZ
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_governance_contracts_name_version_org
    ON governance_contracts (name, version, COALESCE(org_id, ''));

CREATE INDEX IF NOT EXISTS idx_governance_contracts_active
    ON governance_contracts (name, status, org_id);

CREATE TABLE IF NOT EXISTS governance_contract_validation_reports (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id TEXT,
    contract_name TEXT NOT NULL,
    contract_version TEXT NOT NULL,
    subject_type TEXT NOT NULL,
    subject_id TEXT,
    mode TEXT NOT NULL CHECK (mode IN ('report_only', 'enforce')),
    valid BOOLEAN NOT NULL,
    errors JSONB NOT NULL DEFAULT '[]',
    payload_hash TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_contract_validation_reports_org_created
    ON governance_contract_validation_reports (org_id, created_at DESC);

-- Tamper-evident audit metadata. Existing rows remain legacy-unsealed.
ALTER TABLE request_events
    ADD COLUMN IF NOT EXISTS chain_scope TEXT,
    ADD COLUMN IF NOT EXISTS previous_hash TEXT,
    ADD COLUMN IF NOT EXISTS payload_hash TEXT,
    ADD COLUMN IF NOT EXISTS event_hash TEXT,
    ADD COLUMN IF NOT EXISTS signature_key_id TEXT,
    ADD COLUMN IF NOT EXISTS signature TEXT;

CREATE INDEX IF NOT EXISTS idx_request_events_chain_scope_created
    ON request_events (chain_scope, created_at ASC, id ASC);

CREATE UNIQUE INDEX IF NOT EXISTS idx_request_events_event_hash
    ON request_events (event_hash)
    WHERE event_hash IS NOT NULL;

CREATE TABLE IF NOT EXISTS audit_integrity_checks (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    scope TEXT NOT NULL,
    scope_id TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('ok', 'failed')),
    checked_events INTEGER NOT NULL DEFAULT 0,
    first_event_hash TEXT,
    last_event_hash TEXT,
    error_message TEXT,
    checked_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_audit_integrity_checks_scope_checked
    ON audit_integrity_checks (scope, scope_id, checked_at DESC);

-- Durable outbox for callback delivery.
CREATE TABLE IF NOT EXISTS nexus_outbox_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id TEXT,
    event_type TEXT NOT NULL,
    subject_type TEXT NOT NULL,
    subject_id TEXT NOT NULL,
    payload JSONB NOT NULL DEFAULT '{}',
    status TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'delivering', 'delivered', 'dead')),
    attempts INTEGER NOT NULL DEFAULT 0,
    max_attempts INTEGER NOT NULL DEFAULT 10,
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_error TEXT,
    idempotency_key TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_nexus_outbox_idempotency
    ON nexus_outbox_events (idempotency_key);

CREATE INDEX IF NOT EXISTS idx_nexus_outbox_due
    ON nexus_outbox_events (status, next_attempt_at);

CREATE TABLE IF NOT EXISTS nexus_callback_deliveries (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    outbox_event_id UUID NOT NULL REFERENCES nexus_outbox_events(id) ON DELETE CASCADE,
    target_url TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'delivering', 'delivered', 'dead')),
    attempts INTEGER NOT NULL DEFAULT 0,
    max_attempts INTEGER NOT NULL DEFAULT 10,
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_error TEXT,
    response_status INTEGER,
    response_body_hash TEXT,
    delivered_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (outbox_event_id, target_url)
);

CREATE INDEX IF NOT EXISTS idx_nexus_callback_deliveries_due
    ON nexus_callback_deliveries (status, next_attempt_at);

-- Policy lifecycle v2 compatibility layer.
CREATE TABLE IF NOT EXISTS policy_artifacts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    legacy_policy_id UUID UNIQUE REFERENCES policies(id) ON DELETE SET NULL,
    org_id TEXT,
    name TEXT NOT NULL,
    description TEXT,
    current_version_id UUID,
    lifecycle_status TEXT NOT NULL DEFAULT 'draft'
        CHECK (lifecycle_status IN ('draft', 'shadow', 'enforced', 'deprecated', 'archived')),
    created_by TEXT NOT NULL DEFAULT 'system',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_policy_artifacts_org_status
    ON policy_artifacts (org_id, lifecycle_status);

CREATE TABLE IF NOT EXISTS policy_versions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    policy_artifact_id UUID NOT NULL REFERENCES policy_artifacts(id) ON DELETE CASCADE,
    version INTEGER NOT NULL,
    expression TEXT NOT NULL,
    effect TEXT NOT NULL,
    risk_override TEXT,
    priority INTEGER NOT NULL DEFAULT 100,
    action_type TEXT,
    target_system TEXT,
    mode TEXT NOT NULL DEFAULT 'enforced' CHECK (mode IN ('enforced', 'shadow')),
    enabled BOOLEAN NOT NULL DEFAULT true,
    status TEXT NOT NULL DEFAULT 'draft'
        CHECK (status IN ('draft', 'shadow', 'enforced', 'deprecated', 'archived')),
    content_hash TEXT NOT NULL,
    created_by TEXT NOT NULL DEFAULT 'system',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (policy_artifact_id, version)
);

CREATE INDEX IF NOT EXISTS idx_policy_versions_artifact_status
    ON policy_versions (policy_artifact_id, status, version DESC);

ALTER TABLE policy_artifacts
    ADD CONSTRAINT fk_policy_artifacts_current_version
    FOREIGN KEY (current_version_id) REFERENCES policy_versions(id);

CREATE TABLE IF NOT EXISTS policy_changelog (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    policy_artifact_id UUID NOT NULL REFERENCES policy_artifacts(id) ON DELETE CASCADE,
    policy_version_id UUID REFERENCES policy_versions(id) ON DELETE SET NULL,
    actor_id TEXT NOT NULL DEFAULT 'system',
    action TEXT NOT NULL,
    summary TEXT NOT NULL,
    data JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_policy_changelog_artifact_created
    ON policy_changelog (policy_artifact_id, created_at DESC);

CREATE TABLE IF NOT EXISTS policy_promotions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    policy_artifact_id UUID NOT NULL REFERENCES policy_artifacts(id) ON DELETE CASCADE,
    from_version_id UUID REFERENCES policy_versions(id) ON DELETE SET NULL,
    to_version_id UUID NOT NULL REFERENCES policy_versions(id) ON DELETE CASCADE,
    from_status TEXT,
    to_status TEXT NOT NULL,
    requested_by TEXT NOT NULL DEFAULT 'system',
    approved_by TEXT,
    reason TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Backfill existing mutable policies into lifecycle artifacts/version 1.
INSERT INTO policy_artifacts (legacy_policy_id, org_id, name, description, lifecycle_status, created_by, created_at, updated_at)
SELECT
    p.id,
    p.org_id,
    p.name,
    p.description,
    CASE
        WHEN p.archived_at IS NOT NULL THEN 'archived'
        WHEN p.enabled = false THEN 'deprecated'
        WHEN p.mode = 'shadow' THEN 'shadow'
        ELSE 'enforced'
    END,
    'migration',
    p.created_at,
    p.updated_at
FROM policies p
ON CONFLICT (legacy_policy_id) DO NOTHING;

INSERT INTO policy_versions (
    policy_artifact_id, version, expression, effect, risk_override, priority,
    action_type, target_system, mode, enabled, status, content_hash, created_by, created_at
)
SELECT
    a.id,
    1,
    p.expression,
    p.effect,
    p.risk_override,
    p.priority,
    p.action_type,
    p.target_system,
    p.mode,
    p.enabled,
    CASE
        WHEN p.archived_at IS NOT NULL THEN 'archived'
        WHEN p.enabled = false THEN 'deprecated'
        WHEN p.mode = 'shadow' THEN 'shadow'
        ELSE 'enforced'
    END,
    md5(
        COALESCE(p.name, '') || '|' ||
        COALESCE(p.description, '') || '|' ||
        COALESCE(p.action_type, '') || '|' ||
        COALESCE(p.target_system, '') || '|' ||
        COALESCE(p.expression, '') || '|' ||
        COALESCE(p.effect, '') || '|' ||
        COALESCE(p.risk_override, '') || '|' ||
        p.priority::text || '|' ||
        COALESCE(p.mode, '') || '|' ||
        p.enabled::text
    ),
    'migration',
    p.created_at
FROM policies p
JOIN policy_artifacts a ON a.legacy_policy_id = p.id
ON CONFLICT (policy_artifact_id, version) DO NOTHING;

UPDATE policy_artifacts a
SET current_version_id = v.id
FROM policy_versions v
WHERE v.policy_artifact_id = a.id
  AND v.version = 1
  AND a.current_version_id IS NULL;

INSERT INTO policy_changelog (policy_artifact_id, policy_version_id, actor_id, action, summary, data, created_at)
SELECT a.id, v.id, 'migration', 'backfill', 'Backfilled legacy policy into lifecycle v2',
       jsonb_build_object('legacy_policy_id', p.id::text),
       now()
FROM policies p
JOIN policy_artifacts a ON a.legacy_policy_id = p.id
JOIN policy_versions v ON v.policy_artifact_id = a.id AND v.version = 1
ON CONFLICT DO NOTHING;

-- Approval workflow v2 snapshots.
CREATE TABLE IF NOT EXISTS approval_plans (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    approval_id UUID NOT NULL UNIQUE REFERENCES approvals(id) ON DELETE CASCADE,
    org_id TEXT,
    request_id UUID NOT NULL REFERENCES requests(id) ON DELETE CASCADE,
    status TEXT NOT NULL DEFAULT 'active'
        CHECK (status IN ('active', 'completed', 'expired', 'cancelled')),
    required_approvals INTEGER NOT NULL DEFAULT 1,
    separation_of_duties BOOLEAN NOT NULL DEFAULT true,
    stages JSONB NOT NULL DEFAULT '[]',
    constraints JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_approval_plans_org_status
    ON approval_plans (org_id, status, created_at DESC);

CREATE TABLE IF NOT EXISTS break_glass_reviews (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    approval_id UUID NOT NULL REFERENCES approvals(id) ON DELETE CASCADE,
    org_id TEXT,
    request_id UUID NOT NULL REFERENCES requests(id) ON DELETE CASCADE,
    status TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'completed', 'waived')),
    justification TEXT,
    reviewed_by TEXT,
    review_note TEXT,
    reviewed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_break_glass_reviews_org_status
    ON break_glass_reviews (org_id, status, created_at DESC);

-- Operations/rate-limit foundations.
CREATE TABLE IF NOT EXISTS nexus_rate_limit_rules (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id TEXT,
    principal_id TEXT,
    action_type TEXT,
    endpoint TEXT,
    window_seconds INTEGER NOT NULL,
    max_requests INTEGER NOT NULL,
    mode TEXT NOT NULL DEFAULT 'report_only'
        CHECK (mode IN ('report_only', 'enforce')),
    enabled BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_nexus_rate_limit_rules_lookup
    ON nexus_rate_limit_rules (enabled, org_id, principal_id, action_type, endpoint);

CREATE TABLE IF NOT EXISTS nexus_operation_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id TEXT,
    event_type TEXT NOT NULL,
    severity TEXT NOT NULL DEFAULT 'info'
        CHECK (severity IN ('debug', 'info', 'warn', 'error')),
    subject_type TEXT,
    subject_id TEXT,
    data JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_nexus_operation_events_org_created
    ON nexus_operation_events (org_id, created_at DESC);

-- Seed current generic contracts in report-only mode.
INSERT INTO governance_contracts (name, version, subject_type, schema_json, status, validation_mode, compatibility, created_by, promoted_at)
VALUES
    (
        'tool_intent',
        'tool_intent.v1',
        'action_binding',
        '{
            "type": "object",
            "additionalProperties": true,
            "required": [
                "schema_version", "org_id", "actor_id", "actor_type", "product_surface",
                "run_id", "tool_invocation_id", "connector_id", "capability_id",
                "operation", "target_system", "target_resource", "payload_hash",
                "idempotency_key"
            ],
            "properties": {
                "schema_version": {"type": "string", "const": "tool_intent.v1"},
                "org_id": {"type": "string", "minLength": 1},
                "actor_id": {"type": "string", "minLength": 1},
                "actor_type": {"type": "string", "minLength": 1},
                "product_surface": {"type": "string", "minLength": 1},
                "run_id": {"type": "string", "minLength": 1},
                "tool_invocation_id": {"type": "string", "minLength": 1},
                "connector_id": {"type": "string", "minLength": 1},
                "capability_id": {"type": "string", "minLength": 1},
                "operation": {"type": "string", "minLength": 1},
                "target_system": {"type": "string"},
                "target_resource": {"type": "string"},
                "payload_hash": {"type": "string", "minLength": 1},
                "idempotency_key": {"type": "string", "minLength": 1}
            }
        }'::jsonb,
        'active',
        'report_only',
        'backward',
        'migration',
        now()
    ),
    (
        'result_report',
        'result_report.v1',
        'result_report',
        '{
            "type": "object",
            "additionalProperties": true,
            "required": ["success"],
            "properties": {
                "result_id": {"type": "string"},
                "success": {"type": "boolean"},
                "result": {"type": "object"},
                "duration_ms": {"type": "integer"},
                "error_message": {"type": "string"}
            }
        }'::jsonb,
        'active',
        'report_only',
        'backward',
        'migration',
        now()
    )
ON CONFLICT DO NOTHING;
