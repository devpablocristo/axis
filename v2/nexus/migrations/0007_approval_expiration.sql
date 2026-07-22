SET lock_timeout = '5s';
SET statement_timeout = '30s';

ALTER TABLE approvals ADD COLUMN IF NOT EXISTS expires_at timestamptz;

UPDATE approvals
SET expires_at = created_at + interval '1 hour'
WHERE expires_at IS NULL;

ALTER TABLE approvals ALTER COLUMN expires_at SET NOT NULL;
ALTER TABLE approvals DROP CONSTRAINT IF EXISTS approvals_status_check;
ALTER TABLE approvals ADD CONSTRAINT approvals_status_check CHECK (
    status IN ('pending', 'approved', 'rejected', 'expired')
) NOT VALID;
ALTER TABLE approvals VALIDATE CONSTRAINT approvals_status_check;

CREATE INDEX IF NOT EXISTS idx_approvals_due_expiration
    ON approvals (expires_at, id)
    WHERE status = 'pending';
