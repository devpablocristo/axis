CREATE TABLE IF NOT EXISTS companion_virployee_handoffs (
    handoff_id        UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id         UUID NOT NULL,
    org_id            TEXT NOT NULL,
    product_surface   TEXT NOT NULL,
    task_id           UUID,
    from_virployee_id  UUID,
    to_virployee_id    UUID NOT NULL,
    reason            TEXT NOT NULL DEFAULT '',
    status            TEXT NOT NULL DEFAULT 'pending',
    created_by        TEXT NOT NULL DEFAULT '',
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    resolved_at       TIMESTAMPTZ,
    CONSTRAINT companion_virployee_handoffs_org_required CHECK (org_id <> ''),
    CONSTRAINT companion_virployee_handoffs_product_required CHECK (product_surface <> ''),
    CONSTRAINT companion_virployee_handoffs_status_check CHECK (status IN ('pending', 'accepted', 'rejected', 'cancelled'))
);

CREATE INDEX IF NOT EXISTS idx_companion_virployee_handoffs_tenant_status
    ON companion_virployee_handoffs (tenant_id, status, updated_at DESC);

CREATE INDEX IF NOT EXISTS idx_companion_virployee_handoffs_org_surface_status
    ON companion_virployee_handoffs (org_id, product_surface, status, updated_at DESC);

CREATE INDEX IF NOT EXISTS idx_companion_virployee_handoffs_to_virployee
    ON companion_virployee_handoffs (to_virployee_id, updated_at DESC);

CREATE INDEX IF NOT EXISTS idx_companion_virployee_handoffs_task
    ON companion_virployee_handoffs (task_id)
    WHERE task_id IS NOT NULL;
