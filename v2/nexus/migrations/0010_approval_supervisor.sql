SET lock_timeout = '5s';
SET statement_timeout = '30s';

-- The assigned human supervisor is copied from Companion when the governance
-- check creates an approval. Nexus then has an immutable, tenant-local SoD
-- fact and does not need a network lookup while deciding the approval.
ALTER TABLE approvals
    ADD COLUMN IF NOT EXISTS supervisor_user_id text NOT NULL DEFAULT '';
