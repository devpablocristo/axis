-- Tenant fail-closed guardrails. NOT VALID permite desplegar primero y
-- validar/backfillear datos existentes después; las escrituras nuevas ya quedan cerradas.

UPDATE companion_connectors
SET org_id = '__global_disabled__',
    enabled = false
WHERE org_id = '';

DROP INDEX IF EXISTS idx_connectors_kind;
CREATE UNIQUE INDEX IF NOT EXISTS idx_connectors_org_kind_unique
    ON companion_connectors (org_id, kind);

ALTER TABLE companion_connectors
    DROP CONSTRAINT IF EXISTS companion_connectors_org_required,
    ADD CONSTRAINT companion_connectors_org_required CHECK (org_id <> '') NOT VALID;

ALTER TABLE companion_connector_executions
    DROP CONSTRAINT IF EXISTS companion_connector_executions_org_required,
    ADD CONSTRAINT companion_connector_executions_org_required CHECK (org_id <> '') NOT VALID,
    DROP CONSTRAINT IF EXISTS companion_connector_executions_actor_required,
    ADD CONSTRAINT companion_connector_executions_actor_required CHECK (actor_id <> '') NOT VALID;

ALTER TABLE companion_tasks
    DROP CONSTRAINT IF EXISTS companion_tasks_org_required,
    ADD CONSTRAINT companion_tasks_org_required CHECK (org_id <> '') NOT VALID;

ALTER TABLE companion_watchers
    DROP CONSTRAINT IF EXISTS companion_watchers_org_required,
    ADD CONSTRAINT companion_watchers_org_required CHECK (org_id <> '') NOT VALID;

ALTER TABLE companion_proposals
    DROP CONSTRAINT IF EXISTS companion_proposals_org_required,
    ADD CONSTRAINT companion_proposals_org_required CHECK (org_id <> '') NOT VALID;

ALTER TABLE companion_run_traces
    DROP CONSTRAINT IF EXISTS companion_run_traces_org_required,
    ADD CONSTRAINT companion_run_traces_org_required CHECK (org_id <> '') NOT VALID,
    DROP CONSTRAINT IF EXISTS companion_run_traces_product_required,
    ADD CONSTRAINT companion_run_traces_product_required CHECK (product_surface <> '') NOT VALID;
