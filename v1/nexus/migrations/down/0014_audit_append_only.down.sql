-- Reversa de 0014_audit_append_only.up.sql
DROP TRIGGER IF EXISTS request_events_no_truncate ON request_events;
DROP TRIGGER IF EXISTS request_events_no_delete ON request_events;
DROP TRIGGER IF EXISTS request_events_no_update ON request_events;
DROP FUNCTION IF EXISTS request_events_reject_mutation();
