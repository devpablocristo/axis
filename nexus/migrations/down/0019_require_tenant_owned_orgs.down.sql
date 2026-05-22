DROP INDEX IF EXISTS idx_approvals_org_status_created;
DROP INDEX IF EXISTS idx_requests_org_status_created;

ALTER TABLE policy_proposals
    DROP CONSTRAINT IF EXISTS policy_proposals_org_required;

ALTER TABLE request_result_reports
    DROP CONSTRAINT IF EXISTS request_result_reports_org_required;

ALTER TABLE approvals
    DROP CONSTRAINT IF EXISTS approvals_org_required;

ALTER TABLE requests
    DROP CONSTRAINT IF EXISTS requests_org_required;
