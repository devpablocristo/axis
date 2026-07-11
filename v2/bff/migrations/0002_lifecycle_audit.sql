-- platform:migrate:non-transactional
SET lock_timeout = '5s';
SET statement_timeout = '30s';

ALTER TABLE axis_users
    ADD COLUMN IF NOT EXISTS archived_at timestamptz,
    ADD COLUMN IF NOT EXISTS trashed_at timestamptz,
    ADD COLUMN IF NOT EXISTS purge_after timestamptz;

ALTER TABLE axis_orgs
    ADD COLUMN IF NOT EXISTS archived_at timestamptz,
    ADD COLUMN IF NOT EXISTS trashed_at timestamptz,
    ADD COLUMN IF NOT EXISTS purge_after timestamptz;

ALTER TABLE axis_tenants
    ADD COLUMN IF NOT EXISTS archived_at timestamptz,
    ADD COLUMN IF NOT EXISTS trashed_at timestamptz,
    ADD COLUMN IF NOT EXISTS purge_after timestamptz;

ALTER TABLE axis_tenant_members
    ADD COLUMN IF NOT EXISTS archived_at timestamptz,
    ADD COLUMN IF NOT EXISTS trashed_at timestamptz,
    ADD COLUMN IF NOT EXISTS purge_after timestamptz;

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_axis_users_purge_after
    ON axis_users (purge_after)
    WHERE purge_after IS NOT NULL;

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_axis_orgs_purge_after
    ON axis_orgs (purge_after)
    WHERE purge_after IS NOT NULL;

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_axis_tenants_purge_after
    ON axis_tenants (purge_after)
    WHERE purge_after IS NOT NULL;

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_axis_tenant_members_purge_after
    ON axis_tenant_members (purge_after)
    WHERE purge_after IS NOT NULL;
