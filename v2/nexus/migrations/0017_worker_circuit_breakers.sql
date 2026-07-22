SET lock_timeout = '5s';
SET statement_timeout = '30s';

-- This forward-only guard also upgrades development databases that applied the
-- initial enterprise-operations migration while it was still being assembled.
CREATE TABLE IF NOT EXISTS nexus_worker_controls (
    tenant_id text NOT NULL,
    product_surface text NOT NULL,
    kind text NOT NULL,
    state text NOT NULL DEFAULT 'closed' CHECK (state IN ('closed','open','half_open','paused')),
    failure_count integer NOT NULL DEFAULT 0 CHECK (failure_count >= 0),
    failure_window_started_at timestamptz NULL,
    opened_until timestamptz NULL,
    revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
    changed_by text NOT NULL DEFAULT 'system',
    reason_code text NOT NULL DEFAULT 'initialized',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY(tenant_id,product_surface,kind)
);

CREATE TABLE IF NOT EXISTS nexus_job_definitions (
    product_surface text NOT NULL,
    kind text NOT NULL,
    effect_class text NOT NULL CHECK (effect_class IN ('read','internal_write','external_write')),
    replay_policy text NOT NULL CHECK (replay_policy IN ('automatic','operator','forbidden')),
    idempotency_required boolean NOT NULL DEFAULT false,
    protected boolean NOT NULL DEFAULT false,
    PRIMARY KEY(product_surface,kind)
);

INSERT INTO nexus_job_definitions(product_surface,kind,effect_class,replay_policy,idempotency_required,protected)
VALUES ('nexus','approval.expire','internal_write','automatic',true,true),
       ('nexus','ops.governance_reconcile','internal_write','automatic',true,true),
       ('nexus','enterprise.export','internal_write','operator',true,false)
ON CONFLICT(product_surface,kind) DO NOTHING;
