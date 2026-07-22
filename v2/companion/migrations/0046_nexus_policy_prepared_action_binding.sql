SET lock_timeout = '5s';
SET statement_timeout = '30s';

ALTER TABLE companion_prepared_actions
    ADD COLUMN IF NOT EXISTS nexus_policy_snapshot_hash text NOT NULL DEFAULT '';

COMMENT ON COLUMN companion_prepared_actions.nexus_policy_snapshot_hash IS
    'Immutable Nexus governance-policy snapshot approved for this prepared action.';
