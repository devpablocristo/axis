DROP INDEX IF EXISTS idx_requests_binding_hash;

ALTER TABLE requests
    DROP COLUMN IF EXISTS binding_hash,
    DROP COLUMN IF EXISTS action_binding;
