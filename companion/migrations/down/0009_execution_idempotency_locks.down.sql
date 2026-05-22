-- Reversa de 0009_execution_idempotency_locks.up.sql
DROP INDEX IF EXISTS idx_executions_idempotency_unique;
DROP INDEX IF EXISTS idx_executions_idempotency_lookup;

-- Restaurar el índice partial que tenía 0007.
CREATE INDEX IF NOT EXISTS idx_executions_idempotency_lookup
    ON companion_connector_executions (task_id, operation, review_request_id, idempotency_key)
    WHERE task_id IS NOT NULL
      AND review_request_id IS NOT NULL
      AND idempotency_key <> '';

DROP TABLE IF EXISTS companion_connector_execution_locks;
