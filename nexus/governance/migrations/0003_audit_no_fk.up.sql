-- Los audit events son append-only y se emiten antes de crear la request.
-- Remover FK para permitir emisión best-effort en cualquier momento.
ALTER TABLE request_events DROP CONSTRAINT IF EXISTS request_events_request_id_fkey;
