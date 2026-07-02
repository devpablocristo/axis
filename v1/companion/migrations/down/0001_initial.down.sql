-- Reversa de 0001_initial.up.sql (orden inverso por FKs)
DROP INDEX IF EXISTS idx_companion_task_artifacts_task;
DROP TABLE IF EXISTS companion_task_artifacts;

DROP INDEX IF EXISTS idx_companion_task_actions_task;
DROP TABLE IF EXISTS companion_task_actions;

DROP INDEX IF EXISTS idx_companion_task_messages_task;
DROP TABLE IF EXISTS companion_task_messages;

DROP INDEX IF EXISTS idx_companion_tasks_created;
DROP INDEX IF EXISTS idx_companion_tasks_status;
DROP TABLE IF EXISTS companion_tasks;
