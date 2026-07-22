SET lock_timeout = '5s';
SET statement_timeout = '30s';

ALTER TABLE approvals
    ADD COLUMN IF NOT EXISTS product_surface text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS resource_type text NOT NULL DEFAULT '';

COMMENT ON COLUMN approvals.product_surface IS
    'Trusted product scope copied from the governance check for approver authorization.';
COMMENT ON COLUMN approvals.resource_type IS
    'Metadata-only resource type copied from the governance check; target_resource remains an internal reference.';
