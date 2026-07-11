-- platform:migrate:non-transactional
SET lock_timeout = '5s';
SET statement_timeout = '30s';

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_axis_tenants_org_product
    ON axis_tenants (org_id, product_surface);

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_axis_tenant_members_user
    ON axis_tenant_members (user_id, tenant_id);

CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_axis_users_email_lower_unique
    ON axis_users (lower(email))
    WHERE email <> '';

CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_axis_users_provider_user_unique
    ON axis_users (provider, provider_user_id)
    WHERE provider_user_id <> '';

CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_axis_orgs_provider_org_unique
    ON axis_orgs (provider, provider_org_id)
    WHERE provider_org_id <> '';

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_axis_products_purge_after
    ON axis_products (purge_after)
    WHERE purge_after IS NOT NULL;

CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_axis_user_invitations_pending_email
    ON axis_user_invitations (tenant_id, lower(email))
    WHERE status = 'pending' AND archived_at IS NULL AND trashed_at IS NULL;
