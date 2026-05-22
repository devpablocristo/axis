-- Reversa de 0006_task_execution_state.up.sql
DROP INDEX IF EXISTS idx_companion_task_execution_state_retryable;
DROP INDEX IF EXISTS idx_companion_task_execution_state_attempted_at;
DROP TABLE IF EXISTS companion_task_execution_state;
