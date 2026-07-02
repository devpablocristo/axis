-- Delegation graph: quién delega qué a quién
-- Modela la autoridad delegada: owner → agent → action_types → resources
CREATE TABLE IF NOT EXISTS delegations (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id             TEXT NOT NULL,
    owner_type           TEXT NOT NULL DEFAULT 'user',
    agent_id             TEXT NOT NULL,
    agent_type           TEXT NOT NULL DEFAULT 'agent',
    allowed_action_types JSONB NOT NULL DEFAULT '[]',
    allowed_resources    JSONB NOT NULL DEFAULT '[]',
    purpose              TEXT NOT NULL DEFAULT '',
    max_risk_class       TEXT NOT NULL DEFAULT 'high',
    expires_at           TIMESTAMPTZ,
    enabled              BOOLEAN NOT NULL DEFAULT true,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_delegations_agent_id ON delegations (agent_id);
