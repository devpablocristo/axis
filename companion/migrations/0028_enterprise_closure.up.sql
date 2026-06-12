-- Enterprise closure: explicit execution graph replay events.

CREATE TABLE IF NOT EXISTS companion_task_execution_graph_events (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id             TEXT NOT NULL CHECK (btrim(org_id) <> ''),
    task_id            UUID NOT NULL REFERENCES companion_tasks(id) ON DELETE CASCADE,
    step_id            UUID,
    event_type         TEXT NOT NULL CHECK (btrim(event_type) <> ''),
    status             TEXT NOT NULL DEFAULT '',
    agent_id           TEXT NOT NULL DEFAULT '',
    capability_id      TEXT NOT NULL DEFAULT '',
    capability_version TEXT NOT NULL DEFAULT '',
    job_id             UUID,
    nexus_decision_id  TEXT NOT NULL DEFAULT '',
    payload_json       JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_task_execution_graph_task_time
    ON companion_task_execution_graph_events (task_id, created_at ASC);

CREATE INDEX IF NOT EXISTS idx_task_execution_graph_org_time
    ON companion_task_execution_graph_events (org_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_task_execution_graph_step
    ON companion_task_execution_graph_events (step_id, created_at ASC)
    WHERE step_id IS NOT NULL;
