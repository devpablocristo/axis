-- Hace request_events estrictamente append-only a nivel DB.
-- Antes esto se garantizaba solo por convención del código (sin método Update
-- expuesto en el repo) — pero cualquier sesión con permisos sobre la tabla
-- podía igual hacer UPDATE/DELETE y reescribir el audit trail.

CREATE OR REPLACE FUNCTION request_events_reject_mutation()
RETURNS TRIGGER AS $$
BEGIN
    RAISE EXCEPTION 'request_events is append-only: % no permitido', TG_OP
        USING ERRCODE = 'check_violation';
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS request_events_no_update ON request_events;
CREATE TRIGGER request_events_no_update
    BEFORE UPDATE ON request_events
    FOR EACH ROW EXECUTE FUNCTION request_events_reject_mutation();

DROP TRIGGER IF EXISTS request_events_no_delete ON request_events;
CREATE TRIGGER request_events_no_delete
    BEFORE DELETE ON request_events
    FOR EACH ROW EXECUTE FUNCTION request_events_reject_mutation();

-- TRUNCATE no se filtra por triggers per-row; lo bloqueamos a nivel statement.
DROP TRIGGER IF EXISTS request_events_no_truncate ON request_events;
CREATE TRIGGER request_events_no_truncate
    BEFORE TRUNCATE ON request_events
    FOR EACH STATEMENT EXECUTE FUNCTION request_events_reject_mutation();
