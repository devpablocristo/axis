-- Modelo empresarial persistente por customer org/product surface.

CREATE TABLE IF NOT EXISTS companion_business_models (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id          TEXT NOT NULL CHECK (btrim(org_id) <> ''),
    product_surface TEXT NOT NULL DEFAULT 'companion',
    version         INTEGER NOT NULL CHECK (version > 0),
    status          TEXT NOT NULL DEFAULT 'active'
        CHECK (status IN ('active', 'archived')),
    model_json      JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_by      TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (org_id, product_surface, version)
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_business_models_active
    ON companion_business_models (org_id, product_surface)
    WHERE status = 'active';

CREATE TABLE IF NOT EXISTS companion_business_model_audit (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id          TEXT NOT NULL CHECK (btrim(org_id) <> ''),
    product_surface TEXT NOT NULL DEFAULT 'companion',
    version         INTEGER NOT NULL,
    action          TEXT NOT NULL CHECK (btrim(action) <> ''),
    changed_by      TEXT NOT NULL DEFAULT '',
    model_json      JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_business_model_audit_org_version
    ON companion_business_model_audit (org_id, product_surface, version DESC);
