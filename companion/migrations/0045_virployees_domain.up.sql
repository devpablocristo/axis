CREATE TABLE IF NOT EXISTS companion_virployees (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id          UUID NOT NULL,
    org_id             TEXT NOT NULL,
    product_surface    TEXT NOT NULL,
    name               TEXT NOT NULL,
    supervisor_user_id UUID NOT NULL,
    status             TEXT NOT NULL DEFAULT 'draft',
    job_role_id        UUID NOT NULL,
    profile_id         UUID NOT NULL,
    autonomy           TEXT NOT NULL,
    memory_id          UUID,
    created_by         TEXT NOT NULL DEFAULT '',
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    archived_at        TIMESTAMPTZ,
    trashed_at         TIMESTAMPTZ,
    version            INTEGER NOT NULL DEFAULT 1,
    CONSTRAINT companion_virployees_org_required CHECK (org_id <> ''),
    CONSTRAINT companion_virployees_product_required CHECK (product_surface <> ''),
    CONSTRAINT companion_virployees_name_required CHECK (name <> ''),
    CONSTRAINT companion_virployees_status_check CHECK (status IN ('draft', 'active', 'disabled', 'suspended', 'archived', 'trashed', 'error')),
    CONSTRAINT companion_virployees_autonomy_check CHECK (autonomy IN ('A0', 'A1', 'A2', 'A3', 'A4', 'A5'))
);

CREATE INDEX IF NOT EXISTS idx_companion_virployees_tenant_status
    ON companion_virployees (tenant_id, status, updated_at DESC);

CREATE INDEX IF NOT EXISTS idx_companion_virployees_org_surface_status
    ON companion_virployees (org_id, product_surface, status, updated_at DESC);

CREATE TABLE IF NOT EXISTS companion_virployee_capabilities (
    virployee_id   UUID NOT NULL REFERENCES companion_virployees(id) ON DELETE CASCADE,
    capability_id UUID NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (virployee_id, capability_id)
);

CREATE INDEX IF NOT EXISTS idx_companion_virployee_capabilities_capability
    ON companion_virployee_capabilities (capability_id);

CREATE TABLE IF NOT EXISTS companion_virployee_audit (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    virployee_id UUID NOT NULL REFERENCES companion_virployees(id) ON DELETE CASCADE,
    tenant_id   UUID NOT NULL,
    actor_id    TEXT NOT NULL DEFAULT '',
    action      TEXT NOT NULL,
    status      TEXT NOT NULL,
    snapshot    JSONB NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT companion_virployee_audit_action_required CHECK (action <> '')
);

CREATE INDEX IF NOT EXISTS idx_companion_virployee_audit_employee
    ON companion_virployee_audit (virployee_id, created_at DESC);
