ALTER TABLE companion_run_traces
    DROP CONSTRAINT IF EXISTS companion_run_traces_product_required,
    DROP CONSTRAINT IF EXISTS companion_run_traces_org_required;

ALTER TABLE companion_proposals
    DROP CONSTRAINT IF EXISTS companion_proposals_org_required;

ALTER TABLE companion_watchers
    DROP CONSTRAINT IF EXISTS companion_watchers_org_required;

ALTER TABLE companion_tasks
    DROP CONSTRAINT IF EXISTS companion_tasks_org_required;

ALTER TABLE companion_connector_executions
    DROP CONSTRAINT IF EXISTS companion_connector_executions_actor_required,
    DROP CONSTRAINT IF EXISTS companion_connector_executions_org_required;

ALTER TABLE companion_connectors
    DROP CONSTRAINT IF EXISTS companion_connectors_org_required;

DROP INDEX IF EXISTS idx_connectors_org_kind_unique;
CREATE UNIQUE INDEX IF NOT EXISTS idx_connectors_kind
    ON companion_connectors (kind);
