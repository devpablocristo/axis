SET lock_timeout = '5s';
SET statement_timeout = '30s';

-- Tamper-evident, hash-chained ledger of what each virployee did. Events chain
-- per virployee via chain_scope = '<tenant_id>/<virployee_id>': every row's
-- previous_hash points at the prior row's event_hash. data holds only hashes +
-- non-sensitive metadata (never PHI). Made strictly append-only in 0006.
CREATE TABLE IF NOT EXISTS audit_events (
    id uuid PRIMARY KEY,
    tenant_id text NOT NULL,
    chain_scope text NOT NULL,
    virployee_id text NOT NULL,
    subject_type text NOT NULL DEFAULT '',
    subject_id text NOT NULL DEFAULT '',
    event_type text NOT NULL,
    actor_type text NOT NULL DEFAULT '',
    actor_id text NOT NULL DEFAULT '',
    summary text NOT NULL DEFAULT '',
    data jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL,
    previous_hash text NULL,
    payload_hash text NOT NULL,
    event_hash text NOT NULL,
    signature_key_id text NULL,
    signature text NULL,
    CONSTRAINT audit_events_event_hash_unique UNIQUE (event_hash)
);

CREATE INDEX IF NOT EXISTS idx_audit_events_chain ON audit_events (chain_scope, created_at, id);
CREATE INDEX IF NOT EXISTS idx_audit_events_tenant_virployee ON audit_events (tenant_id, virployee_id);
CREATE INDEX IF NOT EXISTS idx_audit_events_subject ON audit_events (chain_scope, subject_id);
