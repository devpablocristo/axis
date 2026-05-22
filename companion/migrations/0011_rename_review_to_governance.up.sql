-- Rename "review" to "governance" across schema (SDK rename consistency).
-- Keeps all data; columns/tables/indexes only change name.

BEGIN;

ALTER TABLE companion_connector_executions
    RENAME COLUMN review_request_id TO governance_request_id;

ALTER TABLE companion_task_actions
    RENAME COLUMN review_request_id TO governance_request_id;

ALTER TABLE companion_proposals
    RENAME COLUMN review_request_id TO governance_request_id;
ALTER TABLE companion_proposals
    RENAME COLUMN review_decision TO governance_decision;

ALTER TABLE companion_task_review_sync_state
    RENAME TO companion_task_governance_sync_state;
ALTER TABLE companion_task_governance_sync_state
    RENAME COLUMN review_request_id TO governance_request_id;
ALTER TABLE companion_task_governance_sync_state
    RENAME COLUMN last_review_status TO last_governance_status;
ALTER TABLE companion_task_governance_sync_state
    RENAME COLUMN last_review_http_status TO last_governance_http_status;
ALTER TABLE companion_task_governance_sync_state
    RENAME CONSTRAINT companion_task_review_sync_failures_check
    TO companion_task_governance_sync_failures_check;

ALTER INDEX idx_executions_review RENAME TO idx_executions_governance;
ALTER INDEX idx_proposals_review RENAME TO idx_proposals_governance;
ALTER INDEX idx_companion_task_review_sync_next_check
    RENAME TO idx_companion_task_governance_sync_next_check;
ALTER INDEX idx_companion_task_review_sync_request
    RENAME TO idx_companion_task_governance_sync_request;

COMMIT;
