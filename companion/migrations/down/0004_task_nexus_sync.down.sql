-- Reversa de 0004_task_nexus_sync.up.sql
DROP INDEX IF EXISTS idx_companion_task_nexus_sync_request;
DROP INDEX IF EXISTS idx_companion_task_nexus_sync_next_check;
DROP TABLE IF EXISTS companion_task_nexus_sync_state;
