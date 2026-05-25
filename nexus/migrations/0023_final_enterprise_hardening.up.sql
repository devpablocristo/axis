-- Final enterprise governance hardening.
-- Additive migration: keeps all existing v1 APIs and semantics compatible.

-- Callback delivery leases and operational lineage.
ALTER TABLE nexus_callback_deliveries
    ADD COLUMN IF NOT EXISTS lease_owner TEXT,
    ADD COLUMN IF NOT EXISTS leased_until TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS last_attempt_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS replayed_from_delivery_id UUID REFERENCES nexus_callback_deliveries(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS poison_reason TEXT;

CREATE INDEX IF NOT EXISTS idx_nexus_callback_deliveries_lease
    ON nexus_callback_deliveries (status, leased_until);

CREATE TABLE IF NOT EXISTS nexus_callback_delivery_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    delivery_id UUID NOT NULL REFERENCES nexus_callback_deliveries(id) ON DELETE CASCADE,
    org_id TEXT,
    event_type TEXT NOT NULL,
    actor_id TEXT NOT NULL DEFAULT 'system',
    data JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_callback_delivery_events_delivery_created
    ON nexus_callback_delivery_events (delivery_id, created_at DESC);

-- Policy promotion control plane.
CREATE TABLE IF NOT EXISTS policy_promotion_requests (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    policy_artifact_id UUID NOT NULL REFERENCES policy_artifacts(id) ON DELETE CASCADE,
    from_version_id UUID REFERENCES policy_versions(id) ON DELETE SET NULL,
    to_version_id UUID NOT NULL REFERENCES policy_versions(id) ON DELETE CASCADE,
    org_id TEXT,
    status TEXT NOT NULL DEFAULT 'draft'
        CHECK (status IN ('draft', 'validated', 'pending_approval', 'approved', 'enforced', 'rolled_back', 'rejected', 'expired', 'failed')),
    requested_by TEXT NOT NULL DEFAULT 'system',
    approved_by TEXT,
    enforced_by TEXT,
    reason TEXT,
    dry_run_report JSONB NOT NULL DEFAULT '{}',
    dry_run_hash TEXT,
    approval_request_id UUID REFERENCES requests(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    approved_at TIMESTAMPTZ,
    enforced_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_policy_promotion_requests_org_status
    ON policy_promotion_requests (org_id, status, created_at DESC);

CREATE TABLE IF NOT EXISTS policy_freeze_windows (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id TEXT,
    starts_at TIMESTAMPTZ NOT NULL,
    ends_at TIMESTAMPTZ NOT NULL,
    reason TEXT NOT NULL DEFAULT '',
    created_by TEXT NOT NULL DEFAULT 'system',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (ends_at > starts_at)
);

CREATE INDEX IF NOT EXISTS idx_policy_freeze_windows_active
    ON policy_freeze_windows (org_id, starts_at, ends_at);

-- Persisted policy simulation reports.
CREATE TABLE IF NOT EXISTS policy_simulation_runs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id TEXT,
    expression TEXT NOT NULL,
    effect TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'completed'
        CHECK (status IN ('pending', 'running', 'completed', 'failed', 'partial')),
    total_evaluated INTEGER NOT NULL DEFAULT 0,
    would_match INTEGER NOT NULL DEFAULT 0,
    would_allow INTEGER NOT NULL DEFAULT 0,
    would_deny INTEGER NOT NULL DEFAULT 0,
    would_require_approval INTEGER NOT NULL DEFAULT 0,
    snapshot_quality TEXT NOT NULL DEFAULT 'current'
        CHECK (snapshot_quality IN ('current', 'legacy', 'partial')),
    report_hash TEXT NOT NULL,
    requested_by TEXT NOT NULL DEFAULT 'system',
    error_message TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_policy_simulation_runs_org_created
    ON policy_simulation_runs (org_id, created_at DESC);

CREATE TABLE IF NOT EXISTS policy_simulation_samples (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    simulation_run_id UUID NOT NULL REFERENCES policy_simulation_runs(id) ON DELETE CASCADE,
    request_id UUID REFERENCES requests(id) ON DELETE SET NULL,
    action_type TEXT NOT NULL DEFAULT '',
    target_system TEXT NOT NULL DEFAULT '',
    original_status TEXT NOT NULL DEFAULT '',
    would_decide TEXT NOT NULL DEFAULT '',
    changed BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_policy_simulation_samples_run
    ON policy_simulation_samples (simulation_run_id);

-- Retention, legal hold and forensic exports.
CREATE TABLE IF NOT EXISTS governance_legal_holds (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id TEXT,
    subject_type TEXT NOT NULL,
    subject_id TEXT NOT NULL DEFAULT '',
    reason TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'active'
        CHECK (status IN ('active', 'released')),
    created_by TEXT NOT NULL DEFAULT 'system',
    released_by TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    released_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_governance_legal_holds_org_status
    ON governance_legal_holds (org_id, status, created_at DESC);

CREATE TABLE IF NOT EXISTS governance_retention_policies (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id TEXT,
    subject_type TEXT NOT NULL,
    retention_days INTEGER NOT NULL CHECK (retention_days > 0),
    mode TEXT NOT NULL DEFAULT 'report_only'
        CHECK (mode IN ('report_only', 'enforce')),
    created_by TEXT NOT NULL DEFAULT 'system',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (org_id, subject_type)
);

CREATE TABLE IF NOT EXISTS governance_export_jobs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id TEXT,
    export_type TEXT NOT NULL CHECK (export_type IN ('audit', 'evidence', 'replay', 'governance_bundle')),
    status TEXT NOT NULL DEFAULT 'completed'
        CHECK (status IN ('pending', 'running', 'completed', 'failed', 'cancelled')),
    subject_type TEXT NOT NULL DEFAULT '',
    subject_id TEXT NOT NULL DEFAULT '',
    requested_by TEXT NOT NULL DEFAULT 'system',
    manifest JSONB NOT NULL DEFAULT '{}',
    manifest_hash TEXT NOT NULL,
    error_message TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_governance_export_jobs_org_created
    ON governance_export_jobs (org_id, created_at DESC);

-- Distributed-safe-enough Postgres rate limiting for Nexus governance endpoints.
CREATE TABLE IF NOT EXISTS nexus_rate_limit_counters (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    rule_id UUID NOT NULL REFERENCES nexus_rate_limit_rules(id) ON DELETE CASCADE,
    bucket_key TEXT NOT NULL,
    window_start TIMESTAMPTZ NOT NULL,
    count INTEGER NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (rule_id, bucket_key, window_start)
);

CREATE INDEX IF NOT EXISTS idx_nexus_rate_limit_counters_window
    ON nexus_rate_limit_counters (window_start, updated_at);

CREATE TABLE IF NOT EXISTS nexus_rate_limit_decisions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id TEXT,
    principal_id TEXT,
    action_type TEXT,
    endpoint TEXT NOT NULL,
    rule_id UUID REFERENCES nexus_rate_limit_rules(id) ON DELETE SET NULL,
    mode TEXT NOT NULL,
    allowed BOOLEAN NOT NULL,
    limit_remaining INTEGER,
    reason TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_nexus_rate_limit_decisions_org_created
    ON nexus_rate_limit_decisions (org_id, created_at DESC);

-- Reconciliation and integrity health reports.
CREATE TABLE IF NOT EXISTS nexus_reconciliation_runs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id TEXT,
    status TEXT NOT NULL DEFAULT 'completed'
        CHECK (status IN ('running', 'completed', 'failed', 'partial')),
    checked_items INTEGER NOT NULL DEFAULT 0,
    finding_count INTEGER NOT NULL DEFAULT 0,
    report_hash TEXT NOT NULL,
    error_message TEXT,
    created_by TEXT NOT NULL DEFAULT 'system',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_nexus_reconciliation_runs_org_created
    ON nexus_reconciliation_runs (org_id, created_at DESC);

CREATE TABLE IF NOT EXISTS nexus_reconciliation_findings (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    run_id UUID NOT NULL REFERENCES nexus_reconciliation_runs(id) ON DELETE CASCADE,
    org_id TEXT,
    severity TEXT NOT NULL CHECK (severity IN ('info', 'warning', 'critical')),
    finding_type TEXT NOT NULL,
    subject_type TEXT NOT NULL,
    subject_id TEXT NOT NULL,
    message TEXT NOT NULL,
    data JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_nexus_reconciliation_findings_org_created
    ON nexus_reconciliation_findings (org_id, created_at DESC);
