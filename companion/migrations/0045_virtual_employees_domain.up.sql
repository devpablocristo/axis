CREATE TABLE IF NOT EXISTS companion_virtual_employees (
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
    CONSTRAINT companion_virtual_employees_org_required CHECK (org_id <> ''),
    CONSTRAINT companion_virtual_employees_product_required CHECK (product_surface <> ''),
    CONSTRAINT companion_virtual_employees_name_required CHECK (name <> ''),
    CONSTRAINT companion_virtual_employees_status_check CHECK (status IN ('draft', 'active', 'disabled', 'suspended', 'archived', 'trashed', 'error')),
    CONSTRAINT companion_virtual_employees_autonomy_check CHECK (autonomy IN ('A0', 'A1', 'A2', 'A3', 'A4', 'A5'))
);

CREATE INDEX IF NOT EXISTS idx_companion_virtual_employees_tenant_status
    ON companion_virtual_employees (tenant_id, status, updated_at DESC);

CREATE INDEX IF NOT EXISTS idx_companion_virtual_employees_org_surface_status
    ON companion_virtual_employees (org_id, product_surface, status, updated_at DESC);

CREATE TABLE IF NOT EXISTS companion_virtual_employee_capabilities (
    employee_id   UUID NOT NULL REFERENCES companion_virtual_employees(id) ON DELETE CASCADE,
    capability_id UUID NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (employee_id, capability_id)
);

CREATE INDEX IF NOT EXISTS idx_companion_virtual_employee_capabilities_capability
    ON companion_virtual_employee_capabilities (capability_id);

CREATE TABLE IF NOT EXISTS companion_virtual_employee_audit (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    employee_id UUID NOT NULL REFERENCES companion_virtual_employees(id) ON DELETE CASCADE,
    tenant_id   UUID NOT NULL,
    actor_id    TEXT NOT NULL DEFAULT '',
    action      TEXT NOT NULL,
    status      TEXT NOT NULL,
    snapshot    JSONB NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT companion_virtual_employee_audit_action_required CHECK (action <> '')
);

CREATE INDEX IF NOT EXISTS idx_companion_virtual_employee_audit_employee
    ON companion_virtual_employee_audit (employee_id, created_at DESC);
