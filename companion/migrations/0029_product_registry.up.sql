-- Multi-product registry and installation control plane.

CREATE TABLE IF NOT EXISTS companion_products (
    product_surface TEXT PRIMARY KEY
        CHECK (product_surface ~ '^[a-z][a-z0-9_-]{0,63}$'),
    display_name    TEXT NOT NULL DEFAULT '',
    status          TEXT NOT NULL DEFAULT 'active'
        CHECK (status IN ('active', 'disabled')),
    metadata_json   JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_by      TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_companion_products_status
    ON companion_products (status, product_surface);

CREATE TABLE IF NOT EXISTS companion_product_installations (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id             TEXT NOT NULL CHECK (btrim(org_id) <> ''),
    product_surface    TEXT NOT NULL
        REFERENCES companion_products (product_surface) ON DELETE CASCADE,
    external_tenant_id TEXT NOT NULL DEFAULT '',
    base_url           TEXT NOT NULL DEFAULT '',
    auth_mode          TEXT NOT NULL DEFAULT 'none'
        CHECK (auth_mode IN ('none', 'api_key_ref', 'oauth2', 'internal_jwt', 'custom')),
    secret_ref         TEXT NOT NULL DEFAULT '',
    enabled            BOOLEAN NOT NULL DEFAULT true,
    config_json        JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_by         TEXT NOT NULL DEFAULT '',
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (org_id, product_surface)
);

CREATE INDEX IF NOT EXISTS idx_companion_product_installations_org
    ON companion_product_installations (org_id, product_surface);

CREATE INDEX IF NOT EXISTS idx_companion_product_installations_enabled
    ON companion_product_installations (org_id, product_surface)
    WHERE enabled = true;

INSERT INTO companion_products (product_surface, display_name, status, metadata_json, created_by)
VALUES
    ('companion', 'Companion', 'active', '{"source":"seed"}'::jsonb, 'migration:0029'),
    ('pymes', 'Pymes', 'active', '{"source":"seed"}'::jsonb, 'migration:0029'),
    ('ponti', 'Ponti', 'active', '{"source":"seed"}'::jsonb, 'migration:0029')
ON CONFLICT (product_surface) DO NOTHING;

ALTER TABLE companion_observability_events
    ADD COLUMN IF NOT EXISTS product_surface TEXT NOT NULL DEFAULT 'companion';

ALTER TABLE companion_cost_events
    ADD COLUMN IF NOT EXISTS product_surface TEXT NOT NULL DEFAULT 'companion';

ALTER TABLE companion_security_eval_reports
    ADD COLUMN IF NOT EXISTS product_surface TEXT NOT NULL DEFAULT 'companion';

UPDATE companion_observability_events event
SET product_surface = trace.product_surface
FROM companion_run_traces trace
WHERE event.run_id = trace.run_id
  AND event.product_surface = 'companion'
  AND btrim(trace.product_surface) <> '';

UPDATE companion_cost_events event
SET product_surface = trace.product_surface
FROM companion_run_traces trace
WHERE event.run_id = trace.run_id
  AND event.product_surface = 'companion'
  AND btrim(trace.product_surface) <> '';

CREATE INDEX IF NOT EXISTS idx_companion_observability_org_product_time
    ON companion_observability_events (org_id, product_surface, occurred_at DESC);

CREATE INDEX IF NOT EXISTS idx_companion_cost_events_org_product_time
    ON companion_cost_events (org_id, product_surface, occurred_at DESC);

CREATE INDEX IF NOT EXISTS idx_security_eval_reports_org_product_time
    ON companion_security_eval_reports (org_id, product_surface, created_at DESC)
    WHERE org_id <> '';
