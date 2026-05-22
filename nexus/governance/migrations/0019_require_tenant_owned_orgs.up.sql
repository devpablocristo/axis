-- Final Axis tenancy boundary: operational data is tenant-owned. This
-- migration fails if existing rows still have no tenant instead of silently
-- treating them as global.

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM requests WHERE org_id IS NULL OR btrim(org_id) = '') THEN
        RAISE EXCEPTION 'requests contains rows without org_id';
    END IF;
    IF EXISTS (SELECT 1 FROM approvals WHERE org_id IS NULL OR btrim(org_id) = '') THEN
        RAISE EXCEPTION 'approvals contains rows without org_id';
    END IF;
    IF EXISTS (SELECT 1 FROM request_result_reports WHERE org_id IS NULL OR btrim(org_id) = '') THEN
        RAISE EXCEPTION 'request_result_reports contains rows without org_id';
    END IF;
    IF EXISTS (SELECT 1 FROM policy_proposals WHERE org_id IS NULL OR btrim(org_id) = '') THEN
        RAISE EXCEPTION 'policy_proposals contains rows without org_id';
    END IF;
END $$;

ALTER TABLE requests
    DROP CONSTRAINT IF EXISTS requests_org_required,
    ADD CONSTRAINT requests_org_required CHECK (org_id IS NOT NULL AND btrim(org_id) <> '') NOT VALID;

ALTER TABLE approvals
    DROP CONSTRAINT IF EXISTS approvals_org_required,
    ADD CONSTRAINT approvals_org_required CHECK (org_id IS NOT NULL AND btrim(org_id) <> '') NOT VALID;

ALTER TABLE request_result_reports
    DROP CONSTRAINT IF EXISTS request_result_reports_org_required,
    ADD CONSTRAINT request_result_reports_org_required CHECK (org_id IS NOT NULL AND btrim(org_id) <> '') NOT VALID;

ALTER TABLE policy_proposals
    DROP CONSTRAINT IF EXISTS policy_proposals_org_required,
    ADD CONSTRAINT policy_proposals_org_required CHECK (org_id IS NOT NULL AND btrim(org_id) <> '') NOT VALID;

CREATE INDEX IF NOT EXISTS idx_requests_org_status_created
    ON requests (org_id, status, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_approvals_org_status_created
    ON approvals (org_id, status, created_at ASC);
