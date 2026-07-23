SET lock_timeout = '5s';
SET statement_timeout = '30s';

CREATE TABLE IF NOT EXISTS companion_business_watchers (
    id uuid PRIMARY KEY,
    org_id text NOT NULL,
    product_id text NOT NULL,
    name text NOT NULL,
    lifecycle text NOT NULL DEFAULT 'draft',
    mode text NOT NULL DEFAULT 'propose',
    active_version_id uuid NULL,
    created_by text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT companion_business_watchers_product_check CHECK (btrim(product_id) <> ''),
    CONSTRAINT companion_business_watchers_name_check CHECK (btrim(name) <> ''),
    CONSTRAINT companion_business_watchers_lifecycle_check CHECK (lifecycle IN ('draft','active','paused','archived')),
    CONSTRAINT companion_business_watchers_mode_check CHECK (mode IN ('observe','propose','execute_if_authorized')),
    CONSTRAINT companion_business_watchers_org_id_unique UNIQUE (org_id,id)
);

CREATE TABLE IF NOT EXISTS companion_business_watcher_versions (
    id uuid PRIMARY KEY,
    org_id text NOT NULL,
    watcher_id uuid NOT NULL,
    version bigint NOT NULL CHECK (version > 0),
    trigger_type text NOT NULL,
    trigger_config jsonb NOT NULL DEFAULT '{}'::jsonb,
    detector_capability_key text NOT NULL,
    detector_manifest_hash text NOT NULL,
    detector_arguments jsonb NOT NULL DEFAULT '{}'::jsonb,
    action_capability_key text NOT NULL DEFAULT '',
    action_manifest_hash text NOT NULL DEFAULT '',
    definition_hash text NOT NULL,
    created_by text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT companion_business_watcher_versions_watcher_fkey
        FOREIGN KEY (org_id,watcher_id) REFERENCES companion_business_watchers(org_id,id),
    CONSTRAINT companion_business_watcher_versions_trigger_check CHECK (trigger_type IN ('schedule','event')),
    CONSTRAINT companion_business_watcher_versions_trigger_config_check CHECK (jsonb_typeof(trigger_config)='object'),
    CONSTRAINT companion_business_watcher_versions_detector_arguments_check CHECK (jsonb_typeof(detector_arguments)='object'),
    CONSTRAINT companion_business_watcher_versions_hash_check CHECK (
        detector_manifest_hash ~ '^[0-9a-f]{64}$'
        AND (action_manifest_hash='' OR action_manifest_hash ~ '^[0-9a-f]{64}$')
        AND definition_hash ~ '^[0-9a-f]{64}$'
    ),
    CONSTRAINT companion_business_watcher_versions_number_unique UNIQUE (org_id,watcher_id,version),
    CONSTRAINT companion_business_watcher_versions_org_id_unique UNIQUE (org_id,id)
);

ALTER TABLE companion_business_watchers
    DROP CONSTRAINT IF EXISTS companion_business_watchers_active_version_fkey;
ALTER TABLE companion_business_watchers
    ADD CONSTRAINT companion_business_watchers_active_version_fkey
        FOREIGN KEY (org_id,active_version_id)
        REFERENCES companion_business_watcher_versions(org_id,id);

CREATE OR REPLACE FUNCTION companion_reject_immutable_artifact_change()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION '% is immutable', TG_TABLE_NAME USING ERRCODE = '55000';
END;
$$;

DROP TRIGGER IF EXISTS companion_business_watcher_versions_immutable
    ON companion_business_watcher_versions;
CREATE TRIGGER companion_business_watcher_versions_immutable
    BEFORE UPDATE OR DELETE ON companion_business_watcher_versions
    FOR EACH ROW EXECUTE FUNCTION companion_reject_immutable_artifact_change();

CREATE TABLE IF NOT EXISTS companion_business_watcher_runs (
    id uuid PRIMARY KEY,
    org_id text NOT NULL,
    watcher_id uuid NOT NULL,
    watcher_version_id uuid NOT NULL,
    trigger_type text NOT NULL,
    trigger_ref_hash text NOT NULL,
    status text NOT NULL,
    detected_count bigint NOT NULL DEFAULT 0,
    proposed_count bigint NOT NULL DEFAULT 0,
    invoked_count bigint NOT NULL DEFAULT 0,
    error_code text NOT NULL DEFAULT '',
    started_at timestamptz NOT NULL DEFAULT now(),
    completed_at timestamptz NULL,
    CONSTRAINT companion_business_watcher_runs_watcher_fkey
        FOREIGN KEY (org_id,watcher_id) REFERENCES companion_business_watchers(org_id,id),
    CONSTRAINT companion_business_watcher_runs_version_fkey
        FOREIGN KEY (org_id,watcher_version_id) REFERENCES companion_business_watcher_versions(org_id,id),
    CONSTRAINT companion_business_watcher_runs_status_check CHECK (status IN ('running','done','failed'))
);

CREATE TABLE IF NOT EXISTS companion_business_watcher_occurrences (
    id uuid PRIMARY KEY,
    org_id text NOT NULL,
    product_id text NOT NULL,
    watcher_id uuid NOT NULL,
    watcher_version_id uuid NOT NULL,
    run_id uuid NOT NULL REFERENCES companion_business_watcher_runs(id),
    occurrence_key text NOT NULL,
    subject_id text NOT NULL DEFAULT '',
    case_id uuid NULL,
    resource_type text NOT NULL DEFAULT '',
    resource_id text NOT NULL DEFAULT '',
    proposal jsonb NOT NULL DEFAULT '{}'::jsonb,
    status text NOT NULL,
    invocation_id text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT companion_business_watcher_occurrences_proposal_check CHECK (jsonb_typeof(proposal)='object'),
    CONSTRAINT companion_business_watcher_occurrences_status_check CHECK (status IN ('observed','proposed','pending_approval','executed','blocked','failed')),
    CONSTRAINT companion_business_watcher_occurrences_dedupe UNIQUE (org_id,watcher_id,occurrence_key)
);

CREATE INDEX IF NOT EXISTS companion_business_watcher_occurrences_recent_idx
    ON companion_business_watcher_occurrences(org_id,product_id,created_at DESC,id DESC);

CREATE TABLE IF NOT EXISTS companion_finops_price_versions (
    id uuid PRIMARY KEY,
    provider text NOT NULL,
    model text NOT NULL,
    input_micro_usd_per_million bigint NOT NULL CHECK (input_micro_usd_per_million >= 0),
    output_micro_usd_per_million bigint NOT NULL CHECK (output_micro_usd_per_million >= 0),
    price_hash text NOT NULL CHECK (price_hash ~ '^[0-9a-f]{64}$'),
    valid_from timestamptz NOT NULL,
    valid_until timestamptz NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT companion_finops_price_versions_range_check CHECK (valid_until IS NULL OR valid_until > valid_from)
);

CREATE TABLE IF NOT EXISTS companion_finops_events (
    id uuid PRIMARY KEY,
    org_id text NOT NULL,
    product_id text NOT NULL DEFAULT '',
    area text NOT NULL,
    service text NOT NULL DEFAULT 'companion',
    virployee_id uuid NULL,
    capability_key text NOT NULL DEFAULT '',
    capability_version text NOT NULL DEFAULT '',
    model text NOT NULL DEFAULT '',
    input_units bigint NOT NULL DEFAULT 0 CHECK (input_units >= 0),
    output_units bigint NOT NULL DEFAULT 0 CHECK (output_units >= 0),
    cost_micro_usd bigint NULL,
    pricing_status text NOT NULL,
    price_version_id uuid NULL REFERENCES companion_finops_price_versions(id),
    price_hash text NOT NULL DEFAULT '',
    event_type text NOT NULL DEFAULT 'usage',
    adjusts_event_id uuid NULL REFERENCES companion_finops_events(id),
    idempotency_key text NOT NULL,
    occurred_at timestamptz NOT NULL,
    recorded_at timestamptz NOT NULL DEFAULT now(),
    metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
    CONSTRAINT companion_finops_events_pricing_check CHECK (pricing_status IN ('priced','unpriced','adjustment')),
    CONSTRAINT companion_finops_events_type_check CHECK (event_type IN ('usage','adjustment')),
    CONSTRAINT companion_finops_events_metadata_check CHECK (jsonb_typeof(metadata)='object'),
    CONSTRAINT companion_finops_events_price_hash_check CHECK (price_hash='' OR price_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT companion_finops_events_dedupe UNIQUE (org_id,idempotency_key)
);

CREATE INDEX IF NOT EXISTS companion_finops_events_summary_idx
    ON companion_finops_events(org_id,occurred_at DESC,product_id,area);

CREATE TABLE IF NOT EXISTS companion_finops_budgets (
    id uuid PRIMARY KEY,
    org_id text NOT NULL,
    scope_type text NOT NULL,
    product_id text NOT NULL DEFAULT '',
    month_start date NOT NULL,
    amount_micro_usd bigint NOT NULL CHECK (amount_micro_usd > 0),
    alert_thresholds jsonb NOT NULL DEFAULT '[80,100]'::jsonb,
    updated_by text NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT companion_finops_budgets_scope_check CHECK (
        (scope_type='organization' AND product_id='') OR
        (scope_type='product' AND btrim(product_id)<>'')
    ),
    CONSTRAINT companion_finops_budgets_thresholds_check CHECK (jsonb_typeof(alert_thresholds)='array'),
    CONSTRAINT companion_finops_budgets_scope_unique UNIQUE (org_id,scope_type,product_id,month_start)
);

COMMENT ON TABLE companion_business_watchers IS
    'Business automation only. Watchers produce at most one governed invocation per occurrence; no task plans or compensations.';
COMMENT ON TABLE companion_finops_events IS
    'Append-only cost attribution. Quotas control work separately; FinOps never authorizes or blocks execution.';
