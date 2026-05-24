-- Cola durable generica para trabajos de Companion.

CREATE TABLE IF NOT EXISTS companion_jobs (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id          TEXT NOT NULL CHECK (btrim(org_id) <> ''),
    kind            TEXT NOT NULL CHECK (btrim(kind) <> ''),
    shard_key       TEXT NOT NULL DEFAULT '',
    dedupe_key      TEXT NOT NULL CHECK (btrim(dedupe_key) <> ''),
    payload_json    JSONB NOT NULL DEFAULT '{}'::jsonb,
    status          TEXT NOT NULL DEFAULT 'queued'
        CHECK (status IN ('queued', 'running', 'succeeded', 'failed', 'dead_letter', 'cancelled')),
    priority        INTEGER NOT NULL DEFAULT 0,
    attempts        INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    max_attempts    INTEGER NOT NULL DEFAULT 3 CHECK (max_attempts > 0),
    run_after       TIMESTAMPTZ NOT NULL DEFAULT now(),
    lease_owner     TEXT NOT NULL DEFAULT '',
    lease_until     TIMESTAMPTZ,
    locked_at       TIMESTAMPTZ,
    heartbeat_at    TIMESTAMPTZ,
    deadline_at     TIMESTAMPTZ,
    timeout_seconds INTEGER NOT NULL DEFAULT 300 CHECK (timeout_seconds > 0),
    last_error      TEXT NOT NULL DEFAULT '',
    evidence_json   JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at    TIMESTAMPTZ
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_companion_jobs_active_dedupe
    ON companion_jobs (dedupe_key)
    WHERE status IN ('queued', 'running');

CREATE INDEX IF NOT EXISTS idx_companion_jobs_claim
    ON companion_jobs (status, run_after, priority DESC, created_at)
    WHERE status IN ('queued', 'running');

CREATE INDEX IF NOT EXISTS idx_companion_jobs_org_kind
    ON companion_jobs (org_id, kind, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_companion_jobs_lease
    ON companion_jobs (lease_until)
    WHERE status = 'running';

CREATE TABLE IF NOT EXISTS companion_job_events (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    job_id       UUID NOT NULL REFERENCES companion_jobs(id) ON DELETE CASCADE,
    event        TEXT NOT NULL CHECK (btrim(event) <> ''),
    payload_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_companion_job_events_job_created
    ON companion_job_events (job_id, created_at);
