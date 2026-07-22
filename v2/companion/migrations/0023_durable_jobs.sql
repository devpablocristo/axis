SET lock_timeout = '5s';
SET statement_timeout = '30s';

CREATE TABLE IF NOT EXISTS companion_jobs (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id text NOT NULL CHECK (btrim(tenant_id) <> ''),
    product_surface text NOT NULL DEFAULT 'companion' CHECK (btrim(product_surface) <> ''),
    kind text NOT NULL CHECK (btrim(kind) <> ''),
    shard_key text NOT NULL DEFAULT '',
    dedupe_key text NOT NULL CHECK (btrim(dedupe_key) <> ''),
    payload_json jsonb NOT NULL DEFAULT '{}'::jsonb,
    status text NOT NULL DEFAULT 'queued'
        CHECK (status IN ('queued', 'running', 'succeeded', 'dead_letter', 'cancelled')),
    priority integer NOT NULL DEFAULT 0,
    attempts integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    max_attempts integer NOT NULL DEFAULT 3 CHECK (max_attempts > 0),
    run_after timestamptz NOT NULL DEFAULT now(),
    lease_owner text NOT NULL DEFAULT '',
    lease_until timestamptz,
    locked_at timestamptz,
    heartbeat_at timestamptz,
    deadline_at timestamptz,
    timeout_seconds integer NOT NULL DEFAULT 300 CHECK (timeout_seconds > 0),
    last_error_code text NOT NULL DEFAULT '',
    evidence_json jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    completed_at timestamptz
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_companion_jobs_dedupe
    ON companion_jobs (tenant_id, product_surface, kind, dedupe_key);

CREATE INDEX IF NOT EXISTS idx_companion_jobs_claim
    ON companion_jobs (priority DESC, run_after, created_at)
    WHERE status IN ('queued', 'running');

CREATE INDEX IF NOT EXISTS idx_companion_jobs_tenant_kind
    ON companion_jobs (tenant_id, product_surface, kind, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_companion_jobs_expired_lease
    ON companion_jobs (lease_until, id)
    WHERE status = 'running';

CREATE TABLE IF NOT EXISTS companion_job_events (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    job_id uuid NOT NULL REFERENCES companion_jobs(id) ON DELETE CASCADE,
    event text NOT NULL CHECK (btrim(event) <> ''),
    metadata_json jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_companion_job_events_job_created
    ON companion_job_events (job_id, created_at, id);

COMMENT ON COLUMN companion_job_events.metadata_json IS
    'Operational metadata only: never payloads, PHI, secrets, signed URLs, or raw errors.';
