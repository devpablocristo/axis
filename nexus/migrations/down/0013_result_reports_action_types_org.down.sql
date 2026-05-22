-- Reversa de 0013_result_reports_action_types_org.up.sql
DROP INDEX IF EXISTS idx_request_result_reports_org_created;
DROP INDEX IF EXISTS idx_request_result_reports_key;
DROP TABLE IF EXISTS request_result_reports;

DROP INDEX IF EXISTS idx_action_types_org_enabled;
DROP INDEX IF EXISTS idx_action_types_org_name;
DROP INDEX IF EXISTS idx_action_types_global_name;

-- Restaurar el unique sobre name a nivel global.
ALTER TABLE action_types ADD CONSTRAINT action_types_name_key UNIQUE (name);
ALTER TABLE action_types DROP COLUMN IF EXISTS org_id;
