-- Reverse the review->governance rename. Pure renames, data preserved.

BEGIN;

ALTER INDEX idx_executions_governance RENAME TO idx_executions_review;
ALTER INDEX idx_proposals_governance RENAME TO idx_proposals_review;
ALTER INDEX idx_companion_task_governance_sync_next_check
    RENAME TO idx_companion_task_review_sync_next_check;
ALTER INDEX idx_companion_task_governance_sync_request
    RENAME TO idx_companion_task_review_sync_request;

ALTER TABLE companion_task_governance_sync_state
    RENAME CONSTRAINT companion_task_governance_sync_failures_check
    TO companion_task_review_sync_failures_check;
ALTER TABLE companion_task_governance_sync_state
    RENAME COLUMN last_governance_http_status TO last_review_http_status;
ALTER TABLE companion_task_governance_sync_state
    RENAME COLUMN last_governance_status TO last_review_status;
ALTER TABLE companion_task_governance_sync_state
    RENAME COLUMN governance_request_id TO review_request_id;
ALTER TABLE companion_task_governance_sync_state
    RENAME TO companion_task_review_sync_state;

ALTER TABLE companion_proposals
    RENAME COLUMN governance_decision TO review_decision;
ALTER TABLE companion_proposals
    RENAME COLUMN governance_request_id TO review_request_id;

ALTER TABLE companion_task_actions
    RENAME COLUMN governance_request_id TO review_request_id;

ALTER TABLE companion_connector_executions
    RENAME COLUMN governance_request_id TO review_request_id;

COMMIT;
