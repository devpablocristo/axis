-- Módulo watchers: observación proactiva del estado del negocio

CREATE TABLE companion_watchers (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id          VARCHAR(255) NOT NULL,
    name            VARCHAR(255) NOT NULL,
    watcher_type    VARCHAR(64)  NOT NULL CHECK (watcher_type IN (
        'stale_work_orders', 'unconfirmed_appointments', 'low_stock',
        'inactive_customers', 'revenue_drop'
    )),
    config          JSONB        NOT NULL DEFAULT '{}',
    enabled         BOOLEAN      NOT NULL DEFAULT true,
    last_run_at     TIMESTAMPTZ,
    last_result     JSONB,
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE INDEX idx_watchers_org_enabled ON companion_watchers (org_id, enabled);
CREATE INDEX idx_watchers_type ON companion_watchers (watcher_type);

CREATE TABLE companion_proposals (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    watcher_id          UUID         NOT NULL REFERENCES companion_watchers(id),
    org_id              VARCHAR(255) NOT NULL,
    action_type         VARCHAR(128) NOT NULL,
    target_resource     VARCHAR(255),
    params              JSONB        NOT NULL DEFAULT '{}',
    reason              TEXT         NOT NULL,
    review_request_id   UUID,
    review_decision     VARCHAR(32),
    execution_status    VARCHAR(32)  NOT NULL DEFAULT 'pending' CHECK (execution_status IN (
        'pending', 'executed', 'failed', 'skipped'
    )),
    execution_result    JSONB,
    created_at          TIMESTAMPTZ  NOT NULL DEFAULT now(),
    resolved_at         TIMESTAMPTZ
);

CREATE INDEX idx_proposals_watcher ON companion_proposals (watcher_id, created_at DESC);
CREATE INDEX idx_proposals_org_status ON companion_proposals (org_id, execution_status);
CREATE INDEX idx_proposals_review ON companion_proposals (review_request_id) WHERE review_request_id IS NOT NULL;
