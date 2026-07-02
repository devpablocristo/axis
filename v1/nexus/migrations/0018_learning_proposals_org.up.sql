-- Scope learned policy proposals to a tenant. Global proposals remain possible
-- only for explicit platform/cross-org flows.

ALTER TABLE policy_proposals
    ADD COLUMN IF NOT EXISTS org_id TEXT;

CREATE INDEX IF NOT EXISTS idx_policy_proposals_org_status
    ON policy_proposals (org_id, status, created_at DESC)
    WHERE org_id IS NOT NULL;
