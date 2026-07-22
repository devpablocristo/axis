SET lock_timeout = '5s';
SET statement_timeout = '30s';

ALTER TABLE governance_checks
    ADD COLUMN IF NOT EXISTS authority_binding_hash text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS scope_revision bigint NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS policy_revision_hash text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS delegation_id text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS delegation_revision bigint NOT NULL DEFAULT 0;

ALTER TABLE approvals
    ADD COLUMN IF NOT EXISTS authority_binding_hash text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS scope_revision bigint NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS policy_revision_hash text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS delegation_id text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS delegation_revision bigint NOT NULL DEFAULT 0;

CREATE INDEX IF NOT EXISTS idx_governance_checks_authority_binding
    ON governance_checks (tenant_id, authority_binding_hash)
    WHERE authority_binding_hash <> '';
