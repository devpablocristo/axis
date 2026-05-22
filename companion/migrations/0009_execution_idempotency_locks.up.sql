-- Stronger idempotency guard for connector executions.

CREATE TABLE IF NOT EXISTS companion_connector_execution_locks (
    lock_key   TEXT PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

DROP INDEX IF EXISTS idx_executions_idempotency_lookup;

CREATE INDEX IF NOT EXISTS idx_executions_idempotency_lookup
    ON companion_connector_executions (task_id, operation, review_request_id, idempotency_key)
    WHERE task_id IS NOT NULL
      AND idempotency_key <> '';

CREATE UNIQUE INDEX IF NOT EXISTS idx_executions_idempotency_unique
    ON companion_connector_executions (task_id, operation, review_request_id, idempotency_key)
    WHERE task_id IS NOT NULL
      AND review_request_id IS NOT NULL
      AND idempotency_key <> '';
