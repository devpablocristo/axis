-- Reversa de 0010_add_org_id.up.sql
DROP INDEX IF EXISTS idx_approvals_org_id;
DROP INDEX IF EXISTS idx_delegations_org_id;
DROP INDEX IF EXISTS idx_policies_org_id;
DROP INDEX IF EXISTS idx_requests_org_id;

ALTER TABLE approvals    DROP COLUMN IF EXISTS org_id;
ALTER TABLE delegations  DROP COLUMN IF EXISTS org_id;
ALTER TABLE policies     DROP COLUMN IF EXISTS org_id;
ALTER TABLE requests     DROP COLUMN IF EXISTS org_id;
