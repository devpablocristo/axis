-- Planes de ejecución manual para tareas aprobadas en Review.

CREATE TABLE companion_task_execution_plans (
    task_id           UUID PRIMARY KEY REFERENCES companion_tasks (id) ON DELETE CASCADE,
    connector_id      UUID NOT NULL REFERENCES companion_connectors (id),
    operation         TEXT NOT NULL,
    payload           JSONB NOT NULL DEFAULT '{}',
    idempotency_key   TEXT NOT NULL DEFAULT '',
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_companion_task_execution_plans_connector
    ON companion_task_execution_plans (connector_id);
