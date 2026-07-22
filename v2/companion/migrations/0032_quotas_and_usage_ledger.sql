SET lock_timeout = '5s';
SET statement_timeout = '30s';

CREATE TABLE IF NOT EXISTS quota_policies (
    tenant_id text NOT NULL,
    product_surface text NOT NULL,
    area text NOT NULL,
    window_seconds integer NOT NULL,
    request_limit bigint NOT NULL,
    unit_limit bigint NOT NULL,
    active boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, product_surface, area),
    CONSTRAINT quota_policies_window_check CHECK (window_seconds BETWEEN 1 AND 86400),
    CONSTRAINT quota_policies_request_limit_check CHECK (request_limit > 0),
    CONSTRAINT quota_policies_unit_limit_check CHECK (unit_limit > 0),
    CONSTRAINT quota_policies_key_check CHECK (
        btrim(tenant_id) <> '' AND btrim(product_surface) <> '' AND btrim(area) <> ''
    )
);

CREATE TABLE IF NOT EXISTS quota_windows (
    tenant_id text NOT NULL,
    product_surface text NOT NULL,
    area text NOT NULL,
    window_started_at timestamptz NOT NULL,
    request_count bigint NOT NULL DEFAULT 0,
    unit_count bigint NOT NULL DEFAULT 0,
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, product_surface, area, window_started_at),
    CONSTRAINT quota_windows_counts_check CHECK (request_count >= 0 AND unit_count >= 0)
);

CREATE TABLE IF NOT EXISTS quota_usage_ledger (
    id uuid PRIMARY KEY,
    tenant_id text NOT NULL,
    product_surface text NOT NULL,
    area text NOT NULL,
    idempotency_key text NOT NULL,
    subject_type text NOT NULL DEFAULT '',
    subject_id text NOT NULL DEFAULT '',
    units bigint NOT NULL,
    allowed boolean NOT NULL DEFAULT false,
    policy_found boolean NOT NULL DEFAULT false,
    retry_after_seconds integer NOT NULL DEFAULT 0,
    model text NOT NULL DEFAULT '',
    estimated_cost_microusd bigint NOT NULL DEFAULT 0,
    metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT quota_usage_units_check CHECK (units >= 0),
    CONSTRAINT quota_usage_cost_check CHECK (estimated_cost_microusd >= 0),
    CONSTRAINT quota_usage_metadata_object_check CHECK (jsonb_typeof(metadata) = 'object'),
    CONSTRAINT quota_usage_idempotency_unique UNIQUE (tenant_id, product_surface, area, idempotency_key)
);

CREATE INDEX IF NOT EXISTS idx_quota_usage_scope_created
    ON quota_usage_ledger (tenant_id, product_surface, area, created_at DESC);

CREATE OR REPLACE FUNCTION reject_quota_usage_mutation()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'quota usage ledger is append-only';
END;
$$;

DROP TRIGGER IF EXISTS quota_usage_reject_update ON quota_usage_ledger;
CREATE TRIGGER quota_usage_reject_update
BEFORE UPDATE ON quota_usage_ledger
FOR EACH ROW EXECUTE FUNCTION reject_quota_usage_mutation();

DROP TRIGGER IF EXISTS quota_usage_reject_delete ON quota_usage_ledger;
CREATE TRIGGER quota_usage_reject_delete
BEFORE DELETE ON quota_usage_ledger
FOR EACH ROW EXECUTE FUNCTION reject_quota_usage_mutation();
