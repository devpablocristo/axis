-- Nexus Review v1: initial schema (RFC 3.0)
-- Order: policies -> policy_proposals -> requests (no approval_id) -> approvals -> alter requests -> request_events -> idempotency_keys

CREATE TABLE policies (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name        TEXT NOT NULL,
    description TEXT,
    action_type TEXT,
    target_system TEXT,
    expression  TEXT NOT NULL,
    effect      TEXT NOT NULL,
    risk_override TEXT,
    priority    INT NOT NULL DEFAULT 100,
    origin      TEXT NOT NULL DEFAULT 'manual',
    proposal_id UUID,
    enabled     BOOLEAN NOT NULL DEFAULT true,
    archived_at TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_policies_active ON policies(enabled, priority) WHERE archived_at IS NULL;

CREATE TABLE policy_proposals (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    proposed_name        TEXT NOT NULL,
    proposed_description TEXT,
    proposed_expression  TEXT NOT NULL,
    proposed_effect      TEXT NOT NULL,
    proposed_action_type TEXT,
    proposed_priority    INT NOT NULL DEFAULT 100,
    pattern_summary      TEXT NOT NULL,
    confidence           FLOAT NOT NULL,
    sample_size          INT NOT NULL,
    time_window          TEXT NOT NULL,
    status               TEXT NOT NULL DEFAULT 'pending',
    decided_by           TEXT,
    decided_at           TIMESTAMPTZ,
    policy_id            UUID,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_proposals_pending ON policy_proposals(status) WHERE status = 'pending';

ALTER TABLE policy_proposals ADD CONSTRAINT fk_proposals_policy FOREIGN KEY (policy_id) REFERENCES policies(id);
ALTER TABLE policies ADD CONSTRAINT fk_policies_proposal FOREIGN KEY (proposal_id) REFERENCES policy_proposals(id);

CREATE TABLE requests (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    idempotency_key TEXT UNIQUE,
    requester_type  TEXT NOT NULL,
    requester_id    TEXT NOT NULL,
    requester_name  TEXT,
    action_type     TEXT NOT NULL,
    target_system   TEXT,
    target_resource TEXT,
    params          JSONB NOT NULL DEFAULT '{}',
    reason          TEXT,
    context         TEXT,
    risk_level      TEXT NOT NULL DEFAULT 'low',
    decision        TEXT NOT NULL,
    decision_reason TEXT,
    policy_id       UUID REFERENCES policies(id),
    status          TEXT NOT NULL DEFAULT 'pending',
    approval_id     UUID,
    execution_result JSONB,
    error_message   TEXT,
    ai_summary      TEXT,
    ai_degraded     BOOLEAN NOT NULL DEFAULT false,
    evaluated_at    TIMESTAMPTZ,
    decided_at      TIMESTAMPTZ,
    executed_at     TIMESTAMPTZ,
    expires_at      TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_requests_requester ON requests(requester_type, requester_id, created_at DESC);
CREATE INDEX idx_requests_status ON requests(status, created_at DESC);
CREATE INDEX idx_requests_action ON requests(action_type, created_at DESC);
CREATE INDEX idx_requests_decision ON requests(decision, created_at DESC);

CREATE TABLE approvals (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    request_id  UUID NOT NULL REFERENCES requests(id),
    status      TEXT NOT NULL DEFAULT 'pending',
    decided_by  TEXT,
    decision_note TEXT,
    decided_at  TIMESTAMPTZ,
    expires_at  TIMESTAMPTZ NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_approvals_pending ON approvals(status, expires_at) WHERE status = 'pending';

ALTER TABLE requests ADD CONSTRAINT fk_requests_approval FOREIGN KEY (approval_id) REFERENCES approvals(id);

CREATE TABLE request_events (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    request_id  UUID NOT NULL REFERENCES requests(id),
    event_type  TEXT NOT NULL,
    actor_type  TEXT NOT NULL,
    actor_id    TEXT NOT NULL,
    summary     TEXT NOT NULL,
    data        JSONB NOT NULL DEFAULT '{}',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_request_events_request ON request_events(request_id, created_at);

CREATE TABLE idempotency_keys (
    key         TEXT PRIMARY KEY,
    request_id  UUID NOT NULL REFERENCES requests(id),
    response    JSONB NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at  TIMESTAMPTZ NOT NULL
);
