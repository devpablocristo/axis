DROP INDEX IF EXISTS idx_policy_proposals_org_status;

ALTER TABLE policy_proposals
    DROP COLUMN IF EXISTS org_id;
