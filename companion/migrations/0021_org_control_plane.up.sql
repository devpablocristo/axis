-- Control plane por customer org para Companion.
--
-- Mantiene los controles operacionales dentro de Companion sin mover decisiones
-- de riesgo final fuera de Nexus.

ALTER TABLE companion_tenant_runtime_policies
    ADD COLUMN IF NOT EXISTS settings_version BIGINT NOT NULL DEFAULT 1 CHECK (settings_version >= 1);

ALTER TABLE companion_tenant_runtime_policies
    ADD COLUMN IF NOT EXISTS control_plane_json JSONB NOT NULL DEFAULT '{}'::jsonb;

CREATE TABLE IF NOT EXISTS companion_runtime_policy_audit (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id           TEXT NOT NULL CHECK (btrim(org_id) <> ''),
    settings_version BIGINT NOT NULL CHECK (settings_version >= 1),
    changed_by       TEXT NOT NULL DEFAULT 'companion.runtime_controls',
    reason           TEXT NOT NULL DEFAULT '',
    policy_json      JSONB NOT NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_companion_runtime_policy_audit_org_version
    ON companion_runtime_policy_audit (org_id, settings_version DESC);
