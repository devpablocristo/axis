SET lock_timeout = '5s';
SET statement_timeout = '30s';

ALTER TABLE companion_runtime_jobs DROP CONSTRAINT IF EXISTS companion_jobs_status_check;
ALTER TABLE companion_runtime_jobs
    ADD COLUMN IF NOT EXISTS cancel_requested_at timestamptz NULL,
    ADD CONSTRAINT companion_jobs_status_check
        CHECK (status IN ('queued','running','cancel_requested','succeeded','dead_letter','cancelled'));

CREATE TABLE IF NOT EXISTS companion_job_definitions (
    product_surface text NOT NULL,
    kind text NOT NULL,
    effect_class text NOT NULL CHECK (effect_class IN ('read','internal_write','external_write')),
    replay_policy text NOT NULL CHECK (replay_policy IN ('automatic','operator','forbidden')),
    idempotency_required boolean NOT NULL DEFAULT false,
    timeout_seconds integer NOT NULL DEFAULT 300 CHECK (timeout_seconds > 0),
    max_attempts integer NOT NULL DEFAULT 3 CHECK (max_attempts > 0),
    allowed_error_codes jsonb NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(allowed_error_codes)='array'),
    protected boolean NOT NULL DEFAULT false,
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (product_surface, kind)
);

INSERT INTO companion_job_definitions(product_surface,kind,effect_class,replay_policy,idempotency_required,protected)
VALUES
    ('companion','assist.process','internal_write','operator',true,false),
    ('companion','operational.watch','internal_write','automatic',true,true),
    ('companion','learning.analyze','internal_write','automatic',true,false),
    ('companion','artifacts.process','internal_write','operator',true,false),
    ('companion','ops.fleet_reconcile','internal_write','automatic',true,true)
ON CONFLICT (product_surface,kind) DO NOTHING;

CREATE TABLE IF NOT EXISTS companion_worker_controls (
    tenant_id text NOT NULL CHECK (btrim(tenant_id)<>''),
    product_surface text NOT NULL CHECK (btrim(product_surface)<>''),
    kind text NOT NULL CHECK (btrim(kind)<>''),
    state text NOT NULL DEFAULT 'closed' CHECK (state IN ('closed','open','half_open','paused')),
    failure_count integer NOT NULL DEFAULT 0 CHECK (failure_count >= 0),
    failure_window_started_at timestamptz NULL,
    opened_until timestamptz NULL,
    revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
    changed_by text NOT NULL DEFAULT 'system',
    reason_code text NOT NULL DEFAULT 'initialized',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, product_surface, kind)
);

CREATE TABLE IF NOT EXISTS companion_fleet_reconciliation_runs (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id text NOT NULL CHECK (btrim(tenant_id)<>''),
    product_surface text NOT NULL CHECK (btrim(product_surface)<>''),
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

CREATE UNIQUE INDEX IF NOT EXISTS idx_companion_fleet_reconciliation_active
    ON companion_fleet_reconciliation_runs(tenant_id,product_surface)
    WHERE status='running';
CREATE INDEX IF NOT EXISTS idx_companion_fleet_reconciliation_recent
    ON companion_fleet_reconciliation_runs(tenant_id,product_surface,started_at DESC,id DESC);

CREATE TABLE IF NOT EXISTS companion_fleet_reconciliation_findings (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    run_id uuid NOT NULL REFERENCES companion_fleet_reconciliation_runs(id) ON DELETE CASCADE,
    tenant_id text NOT NULL,
    finding_type text NOT NULL CHECK (btrim(finding_type)<>''),
    severity text NOT NULL CHECK (severity IN ('info','warning','high','critical')),
    resource_type text NOT NULL,
    resource_id text NOT NULL,
    fingerprint text NOT NULL CHECK (fingerprint ~ '^[0-9a-f]{64}$'),
    expected_hash text NOT NULL DEFAULT '',
    observed_hash text NOT NULL DEFAULT '',
    repair_class text NOT NULL CHECK (repair_class IN ('automatic_safe','manual')),
    repaired boolean NOT NULL DEFAULT false,
    metadata_json jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(metadata_json)='object'),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (run_id, fingerprint)
);

CREATE INDEX IF NOT EXISTS idx_companion_fleet_findings_tenant
    ON companion_fleet_reconciliation_findings(tenant_id,created_at DESC,id DESC);

CREATE TABLE IF NOT EXISTS companion_operation_requests (
    tenant_id text NOT NULL,
    actor_id text NOT NULL,
    idempotency_key text NOT NULL,
    operation text NOT NULL,
    resource_id text NOT NULL DEFAULT '',
    response_json jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, actor_id, idempotency_key)
);

ALTER TABLE companion_nexus_outbox DROP CONSTRAINT IF EXISTS companion_nexus_outbox_type_kind_check;
ALTER TABLE companion_nexus_outbox ADD CONSTRAINT companion_nexus_outbox_type_kind_check CHECK (
    (aggregate_type='execution_attempt' AND kind='execution_result') OR
    (aggregate_type='professional_authority' AND kind='audit_event') OR
    (aggregate_type='operational_finding' AND kind='operational_finding')
);

COMMENT ON COLUMN companion_fleet_reconciliation_findings.metadata_json IS
    'Metadata-only operational evidence; never prompts, documents, arguments, PHI, secrets or raw errors.';
