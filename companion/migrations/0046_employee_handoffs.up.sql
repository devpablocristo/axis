CREATE TABLE IF NOT EXISTS companion_employee_handoffs (
    handoff_id        UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id         UUID NOT NULL,
    org_id            TEXT NOT NULL,
    product_surface   TEXT NOT NULL,
    task_id           UUID,
    from_employee_id  UUID,
    to_employee_id    UUID NOT NULL,
    reason            TEXT NOT NULL DEFAULT '',
    status            TEXT NOT NULL DEFAULT 'pending',
    created_by        TEXT NOT NULL DEFAULT '',
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    resolved_at       TIMESTAMPTZ,
    CONSTRAINT companion_employee_handoffs_org_required CHECK (org_id <> ''),
    CONSTRAINT companion_employee_handoffs_product_required CHECK (product_surface <> ''),
    CONSTRAINT companion_employee_handoffs_status_check CHECK (status IN ('pending', 'accepted', 'rejected', 'cancelled'))
);

CREATE INDEX IF NOT EXISTS idx_companion_employee_handoffs_tenant_status
    ON companion_employee_handoffs (tenant_id, status, updated_at DESC);

CREATE INDEX IF NOT EXISTS idx_companion_employee_handoffs_org_surface_status
    ON companion_employee_handoffs (org_id, product_surface, status, updated_at DESC);

CREATE INDEX IF NOT EXISTS idx_companion_employee_handoffs_to_employee
    ON companion_employee_handoffs (to_employee_id, updated_at DESC);

CREATE INDEX IF NOT EXISTS idx_companion_employee_handoffs_task
    ON companion_employee_handoffs (task_id)
    WHERE task_id IS NOT NULL;
