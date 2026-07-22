SET lock_timeout = '5s';
SET statement_timeout = '30s';

ALTER TABLE operational_notification_outbox
    ADD COLUMN IF NOT EXISTS lease_until timestamptz NULL;

CREATE INDEX IF NOT EXISTS idx_operational_notification_delivery
    ON operational_notification_outbox(status,available_at,lease_until)
    WHERE status IN ('pending','processing');
