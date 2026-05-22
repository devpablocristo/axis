-- Reversa de 0003_memory_connectors.up.sql
DROP INDEX IF EXISTS idx_executions_review;
DROP INDEX IF EXISTS idx_executions_task;
DROP INDEX IF EXISTS idx_executions_connector;
DROP TABLE IF EXISTS companion_connector_executions;

DROP INDEX IF EXISTS idx_connectors_kind;
DROP TABLE IF EXISTS companion_connectors;

DROP INDEX IF EXISTS idx_memory_scope_key;
DROP INDEX IF EXISTS idx_memory_expires;
DROP INDEX IF EXISTS idx_memory_scope_kind;
DROP TABLE IF EXISTS companion_memory_entries;
