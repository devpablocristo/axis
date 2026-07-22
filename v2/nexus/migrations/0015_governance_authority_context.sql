SET lock_timeout = '5s';
SET statement_timeout = '30s';

-- 0013 existed while the advanced-authority envelope was still being built.
-- Keep this migration additive so databases that already recorded 0013 gain
-- the final metadata-only columns without rewriting migration history.
ALTER TABLE governance_checks
    ADD COLUMN IF NOT EXISTS product_surface text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS requester_type text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS resource_type text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS membership_role text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS functional_roles jsonb NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN IF NOT EXISTS functional_scopes jsonb NOT NULL DEFAULT '[]'::jsonb;

COMMENT ON COLUMN governance_checks.functional_roles IS
    'Metadata-only snapshot of functional role keys resolved by Nexus; never accepted from model or client authority claims.';
COMMENT ON COLUMN governance_checks.functional_scopes IS
    'Metadata-only effective grant snapshot used for execution revalidation.';
