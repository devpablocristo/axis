SET lock_timeout = '5s';
SET statement_timeout = '30s';

ALTER TABLE nexus_jobs DROP CONSTRAINT IF EXISTS nexus_jobs_status_check;
ALTER TABLE nexus_jobs
    ADD COLUMN IF NOT EXISTS cancel_requested_at timestamptz NULL,
    ADD CONSTRAINT nexus_jobs_status_check
        CHECK (status IN ('queued','running','cancel_requested','succeeded','dead_letter','cancelled'));

ALTER TABLE functional_role_grants DROP CONSTRAINT IF EXISTS functional_role_grants_role_key_check;
ALTER TABLE functional_role_grants ADD CONSTRAINT functional_role_grants_role_key_check
    CHECK (role_key IN ('policy_admin','approver','auditor','delegation_admin','operator'));

CREATE TABLE IF NOT EXISTS nexus_job_definitions (
    product_surface text NOT NULL,
    kind text NOT NULL,
    effect_class text NOT NULL CHECK (effect_class IN ('read','internal_write','external_write')),
    replay_policy text NOT NULL CHECK (replay_policy IN ('automatic','operator','forbidden')),
    idempotency_required boolean NOT NULL DEFAULT false,
    protected boolean NOT NULL DEFAULT false,
    PRIMARY KEY(product_surface,kind)
);
INSERT INTO nexus_job_definitions(product_surface,kind,effect_class,replay_policy,idempotency_required,protected)
VALUES ('nexus','approval.expire','internal_write','automatic',true,true),
       ('nexus','ops.governance_reconcile','internal_write','automatic',true,true),
       ('nexus','enterprise.export','internal_write','operator',true,false)
ON CONFLICT(product_surface,kind) DO NOTHING;

CREATE TABLE IF NOT EXISTS nexus_worker_controls (
    tenant_id text NOT NULL,
    product_surface text NOT NULL,
    kind text NOT NULL,
    state text NOT NULL DEFAULT 'closed' CHECK (state IN ('closed','open','half_open','paused')),
    failure_count integer NOT NULL DEFAULT 0,
    failure_window_started_at timestamptz NULL,
    opened_until timestamptz NULL,
    revision bigint NOT NULL DEFAULT 1,
    changed_by text NOT NULL DEFAULT 'system',
    reason_code text NOT NULL DEFAULT 'initialized',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY(tenant_id,product_surface,kind)
);

CREATE TABLE IF NOT EXISTS nexus_governance_reconciliation_runs (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id text NOT NULL,
    product_surface text NOT NULL,
    mode text NOT NULL CHECK (mode IN ('detect','safe_repair')),
    trigger text NOT NULL CHECK (trigger IN ('scheduled','manual')),
    status text NOT NULL CHECK (status IN ('running','succeeded','partial','failed')),
    actor_id text NOT NULL,
    idempotency_key text NOT NULL,
    findings_count integer NOT NULL DEFAULT 0,
    repaired_count integer NOT NULL DEFAULT 0,
    report_hash text NOT NULL DEFAULT '',
    error_code text NOT NULL DEFAULT '',
    started_at timestamptz NOT NULL DEFAULT now(),
    completed_at timestamptz NULL,
    UNIQUE (tenant_id, product_surface, idempotency_key)
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_nexus_governance_reconcile_active
    ON nexus_governance_reconciliation_runs(tenant_id,product_surface) WHERE status='running';

CREATE TABLE IF NOT EXISTS nexus_governance_reconciliation_findings (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    run_id uuid NOT NULL REFERENCES nexus_governance_reconciliation_runs(id) ON DELETE CASCADE,
    tenant_id text NOT NULL,
    finding_type text NOT NULL,
    severity text NOT NULL CHECK (severity IN ('info','warning','high','critical')),
    resource_type text NOT NULL,
    resource_id text NOT NULL,
    fingerprint text NOT NULL CHECK (fingerprint ~ '^[0-9a-f]{64}$'),
    repair_class text NOT NULL CHECK (repair_class IN ('automatic_safe','manual')),
    repaired boolean NOT NULL DEFAULT false,
    metadata_json jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(metadata_json)='object'),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (run_id,fingerprint)
);

CREATE TABLE IF NOT EXISTS operational_incidents (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id text NOT NULL,
    fingerprint text NOT NULL CHECK (fingerprint ~ '^[0-9a-f]{64}$'),
    source text NOT NULL,
    incident_type text NOT NULL,
    resource_type text NOT NULL,
    resource_id text NOT NULL,
    severity text NOT NULL CHECK (severity IN ('info','warning','high','critical')),
    status text NOT NULL DEFAULT 'open' CHECK (status IN ('open','acknowledged','suppressed','resolved')),
    occurrence_count bigint NOT NULL DEFAULT 1 CHECK (occurrence_count > 0),
    consecutive_absent_runs integer NOT NULL DEFAULT 0 CHECK (consecutive_absent_runs >= 0),
    state_based boolean NOT NULL DEFAULT true,
    first_seen timestamptz NOT NULL DEFAULT now(),
    last_seen timestamptz NOT NULL DEFAULT now(),
    suppress_until timestamptz NULL,
    revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
    metadata_json jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(metadata_json)='object'),
    UNIQUE (tenant_id,fingerprint)
);
CREATE INDEX IF NOT EXISTS idx_operational_incidents_status
    ON operational_incidents(tenant_id,status,severity,last_seen DESC,id DESC);

CREATE TABLE IF NOT EXISTS operational_incident_events (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id text NOT NULL,
    incident_id uuid NOT NULL REFERENCES operational_incidents(id) ON DELETE CASCADE,
    event_type text NOT NULL CHECK (event_type IN ('opened','observed','acknowledged','suppressed','resolved','reopened')),
    actor_id text NOT NULL,
    reason_code text NOT NULL,
    revision bigint NOT NULL,
    metadata_json jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(metadata_json)='object'),
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS operational_slos (
    tenant_id text NOT NULL,
    product_surface text NOT NULL,
    metric_key text NOT NULL CHECK (metric_key IN (
        'assist_availability','assist_latency_p95','tool_success_rate','job_success_rate',
        'oldest_queue_age','oldest_outbox_age','dlq_count','audit_integrity','quota_consumption'
    )),
    comparator text NOT NULL CHECK (comparator IN ('gte','lte','eq')),
    target double precision NOT NULL,
    window_seconds integer NOT NULL CHECK (window_seconds > 0),
    minimum_samples integer NOT NULL DEFAULT 1 CHECK (minimum_samples > 0),
    severity text NOT NULL CHECK (severity IN ('info','warning','high','critical')),
    enabled boolean NOT NULL DEFAULT false,
    revision bigint NOT NULL DEFAULT 1,
    changed_by text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id,product_surface,metric_key)
);

CREATE TABLE IF NOT EXISTS legal_holds (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id text NOT NULL,
    scope_type text NOT NULL CHECK (scope_type IN ('tenant','virployee','work_subject','case','audit_chain','export')),
    scope_id text NOT NULL,
    reason_code text NOT NULL,
    external_reference text NOT NULL DEFAULT '',
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active','released')),
    revision bigint NOT NULL DEFAULT 1,
    created_by text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    released_by text NOT NULL DEFAULT '',
    released_at timestamptz NULL,
    release_reason text NOT NULL DEFAULT ''
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_legal_holds_active_scope
    ON legal_holds(tenant_id,scope_type,scope_id) WHERE status='active';

CREATE TABLE IF NOT EXISTS legal_hold_events (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id text NOT NULL,
    legal_hold_id uuid NOT NULL REFERENCES legal_holds(id) ON DELETE CASCADE,
    event_type text NOT NULL CHECK (event_type IN ('created','released')),
    actor_id text NOT NULL,
    reason_code text NOT NULL,
    revision bigint NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS enterprise_exports (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id text NOT NULL,
    scope_type text NOT NULL CHECK (scope_type IN ('tenant','virployee','work_subject','case','audit_chain')),
    scope_id text NOT NULL,
    categories_json jsonb NOT NULL CHECK (jsonb_typeof(categories_json)='array'),
    status text NOT NULL DEFAULT 'queued' CHECK (status IN ('queued','running','ready','failed','expired')),
    idempotency_key text NOT NULL,
    manifest_json jsonb NOT NULL DEFAULT '{}'::jsonb,
    manifest_hash text NOT NULL DEFAULT '',
    artifact_ref text NOT NULL DEFAULT '',
    error_code text NOT NULL DEFAULT '',
    requested_by text NOT NULL,
    requested_at timestamptz NOT NULL DEFAULT now(),
    completed_at timestamptz NULL,
    expires_at timestamptz NULL,
    UNIQUE (tenant_id,idempotency_key)
);

CREATE TABLE IF NOT EXISTS enterprise_export_download_tokens (
    token_hash text PRIMARY KEY CHECK (token_hash ~ '^[0-9a-f]{64}$'),
    tenant_id text NOT NULL,
    export_id uuid NOT NULL REFERENCES enterprise_exports(id) ON DELETE CASCADE,
    actor_id text NOT NULL,
    manifest_hash text NOT NULL,
    expires_at timestamptz NOT NULL,
    used_at timestamptz NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS enterprise_export_files (
    export_id uuid NOT NULL REFERENCES enterprise_exports(id) ON DELETE CASCADE,
    file_name text NOT NULL,
    content bytea NOT NULL,
    sha256 text NOT NULL CHECK (sha256 ~ '^[0-9a-f]{64}$'),
    size_bytes bigint NOT NULL CHECK (size_bytes >= 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY(export_id,file_name)
);

CREATE TABLE IF NOT EXISTS operational_notification_policy (
    tenant_id text PRIMARY KEY,
    enabled boolean NOT NULL DEFAULT false,
    webhook_secret_ref text NOT NULL DEFAULT '',
    revision bigint NOT NULL DEFAULT 1,
    changed_by text NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS operational_notification_outbox (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id text NOT NULL,
    incident_id uuid NOT NULL REFERENCES operational_incidents(id) ON DELETE CASCADE,
    event_type text NOT NULL,
    dedupe_key text NOT NULL,
    payload_json jsonb NOT NULL,
    status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','processing','delivered','dead')),
    attempts integer NOT NULL DEFAULT 0,
    available_at timestamptz NOT NULL DEFAULT now(),
    lease_until timestamptz NULL,
    last_error_code text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id,dedupe_key)
);

CREATE TABLE IF NOT EXISTS nexus_operation_requests (
    tenant_id text NOT NULL,
    actor_id text NOT NULL,
    idempotency_key text NOT NULL,
    operation text NOT NULL,
    resource_id text NOT NULL DEFAULT '',
    response_json jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id,actor_id,idempotency_key)
);

COMMENT ON COLUMN operational_incidents.metadata_json IS
    'Metadata-only operational evidence; never payloads, documents, conversations, PHI, secrets or raw errors.';
