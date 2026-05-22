-- Reversa de 0008_task_org_id.up.sql
DROP INDEX IF EXISTS idx_companion_tasks_org_updated;
ALTER TABLE companion_tasks DROP COLUMN IF EXISTS org_id;
