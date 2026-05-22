-- Attestations: prueba verificable de qué ejecutó el sistema target
CREATE TABLE IF NOT EXISTS attestations (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    request_id    UUID NOT NULL REFERENCES requests(id) ON DELETE CASCADE,
    status        TEXT NOT NULL,
    provider_refs JSONB NOT NULL DEFAULT '{}',
    signature     TEXT NOT NULL,
    attester      TEXT NOT NULL,
    metadata      JSONB NOT NULL DEFAULT '{}',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_attestations_request_id ON attestations(request_id);
