SET lock_timeout = '5s';
SET statement_timeout = '30s';

CREATE TABLE IF NOT EXISTS companion_product_integrations (
    id uuid PRIMARY KEY,
    org_id text NOT NULL,
    product_id uuid NOT NULL,
    product_surface text NOT NULL,
    lifecycle text NOT NULL DEFAULT 'draft'
        CHECK (lifecycle IN ('draft','active','suspended','retired')),
    active_version_id uuid,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE(org_id, product_id)
);

CREATE TABLE IF NOT EXISTS companion_product_integration_versions (
    id uuid PRIMARY KEY,
    integration_id uuid NOT NULL REFERENCES companion_product_integrations(id) ON DELETE CASCADE,
    revision bigint NOT NULL,
    source_integration_id uuid NOT NULL,
    source_version_id uuid NOT NULL,
    source_revision bigint NOT NULL,
    contract_hash char(64) NOT NULL,
    section_json jsonb NOT NULL,
    content_hash char(64) NOT NULL,
    status text NOT NULL DEFAULT 'draft'
        CHECK (status IN ('draft','validated','active','retired')),
    created_by text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    activated_by text,
    activated_at timestamptz,
    UNIQUE(integration_id, revision),
    UNIQUE(integration_id, source_version_id)
);

ALTER TABLE companion_product_integrations
    DROP CONSTRAINT IF EXISTS companion_product_integrations_active_version_id_fkey;
ALTER TABLE companion_product_integrations
    ADD CONSTRAINT companion_product_integrations_active_version_id_fkey
    FOREIGN KEY(active_version_id) REFERENCES companion_product_integration_versions(id)
    DEFERRABLE INITIALLY DEFERRED;

CREATE TABLE IF NOT EXISTS companion_product_integration_validation_reports (
    id uuid PRIMARY KEY,
    org_id text NOT NULL,
    product_id uuid NOT NULL,
    version_id uuid NOT NULL REFERENCES companion_product_integration_versions(id) ON DELETE CASCADE,
    content_hash char(64) NOT NULL,
    valid boolean NOT NULL,
    checks_json jsonb NOT NULL,
    created_by text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE(version_id, content_hash)
);

CREATE TABLE IF NOT EXISTS companion_product_service_activity (
    org_id text NOT NULL,
    product_id uuid NOT NULL,
    product_surface text NOT NULL,
    integration_id uuid,
    integration_revision bigint,
    integration_hash char(64),
    area text NOT NULL,
    access_mode text NOT NULL DEFAULT 'direct'
        CHECK (access_mode IN ('direct','via_companion')),
    bucket_start timestamptz NOT NULL,
    request_count bigint NOT NULL DEFAULT 0,
    success_count bigint NOT NULL DEFAULT 0,
    denied_count bigint NOT NULL DEFAULT 0,
    failure_count bigint NOT NULL DEFAULT 0,
    latency_samples_ms integer[] NOT NULL DEFAULT '{}',
    last_seen_at timestamptz NOT NULL,
    last_success_at timestamptz,
    last_error_code text,
    last_error_at timestamptz,
    PRIMARY KEY(org_id,product_id,area,access_mode,bucket_start)
);

CREATE INDEX IF NOT EXISTS idx_companion_product_integrations_org
    ON companion_product_integrations(org_id,lifecycle,product_id);
CREATE INDEX IF NOT EXISTS idx_companion_product_activity_window
    ON companion_product_service_activity(org_id,product_id,bucket_start DESC);
