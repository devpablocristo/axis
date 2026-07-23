SET lock_timeout = '5s';
SET statement_timeout = '30s';

CREATE TABLE IF NOT EXISTS product_integrations (
    id uuid PRIMARY KEY,
    org_id uuid NOT NULL REFERENCES axis_orgs(id) ON DELETE CASCADE,
    product_id uuid NOT NULL REFERENCES axis_products(id) ON DELETE CASCADE,
    lifecycle text NOT NULL DEFAULT 'draft'
        CHECK (lifecycle IN ('draft', 'active', 'suspended', 'retired')),
    active_version_id uuid,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (org_id, product_id)
);

CREATE TABLE IF NOT EXISTS product_integration_versions (
    id uuid PRIMARY KEY,
    integration_id uuid NOT NULL REFERENCES product_integrations(id) ON DELETE CASCADE,
    revision bigint NOT NULL,
    schema_version text NOT NULL,
    contract_json jsonb NOT NULL,
    contract_hash char(64) NOT NULL,
    required_services text[] NOT NULL,
    status text NOT NULL DEFAULT 'draft'
        CHECK (status IN ('draft', 'validated', 'active', 'retired')),
    created_by text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    activated_by text,
    activated_at timestamptz,
    UNIQUE (integration_id, revision),
    UNIQUE (integration_id, contract_hash)
);

ALTER TABLE product_integrations
    DROP CONSTRAINT IF EXISTS product_integrations_active_version_id_fkey;
ALTER TABLE product_integrations
    ADD CONSTRAINT product_integrations_active_version_id_fkey
    FOREIGN KEY (active_version_id)
    REFERENCES product_integration_versions(id)
    DEFERRABLE INITIALLY DEFERRED;

CREATE TABLE IF NOT EXISTS product_integration_validation_reports (
    id uuid PRIMARY KEY,
    org_id uuid NOT NULL,
    product_id uuid NOT NULL,
    version_id uuid NOT NULL REFERENCES product_integration_versions(id) ON DELETE CASCADE,
    contract_hash char(64) NOT NULL,
    valid boolean NOT NULL,
    checks_json jsonb NOT NULL,
    service_snapshots_json jsonb NOT NULL DEFAULT '{}',
    created_by text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_product_integration_reports_version
    ON product_integration_validation_reports(version_id, created_at DESC);

CREATE TABLE IF NOT EXISTS product_credentials (
    id uuid PRIMARY KEY,
    org_id uuid NOT NULL REFERENCES axis_orgs(id) ON DELETE CASCADE,
    product_id uuid NOT NULL REFERENCES axis_products(id) ON DELETE CASCADE,
    integration_id uuid NOT NULL REFERENCES product_integrations(id) ON DELETE CASCADE,
    key_prefix text NOT NULL,
    secret_digest bytea NOT NULL,
    service_principal text NOT NULL,
    scopes text[] NOT NULL,
    status text NOT NULL DEFAULT 'active'
        CHECK (status IN ('active', 'revoked')),
    created_by text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    rotated_at timestamptz,
    revoked_by text,
    revoked_at timestamptz,
    UNIQUE(secret_digest)
);

CREATE INDEX IF NOT EXISTS idx_product_credentials_product
    ON product_credentials(org_id, product_id, status);
