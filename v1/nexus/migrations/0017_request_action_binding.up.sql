-- Persist canonical action bindings so nexus decisions can be tied to the
-- exact action/payload Companion intends to execute.

ALTER TABLE requests
    ADD COLUMN IF NOT EXISTS action_binding JSONB NOT NULL DEFAULT '{}',
    ADD COLUMN IF NOT EXISTS binding_hash TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_requests_binding_hash
    ON requests (binding_hash)
    WHERE binding_hash <> '';
