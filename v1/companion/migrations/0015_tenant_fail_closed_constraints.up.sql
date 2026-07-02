-- Tenant fail-closed guardrails. NOT VALID permite desplegar primero y
-- validar/backfillear datos existentes después; las escrituras nuevas ya quedan cerradas.

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
