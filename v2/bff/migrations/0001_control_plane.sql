CREATE TABLE IF NOT EXISTS axis_users (
    id uuid PRIMARY KEY,
    provider text NOT NULL DEFAULT 'dev',
    provider_user_id text NOT NULL DEFAULT '',
    email text NOT NULL DEFAULT '',
    status text NOT NULL DEFAULT 'active',
    synced_at timestamptz,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL
);

CREATE TABLE IF NOT EXISTS axis_orgs (
    id uuid PRIMARY KEY,
    provider text NOT NULL DEFAULT 'dev',
    provider_org_id text NOT NULL DEFAULT '',
    name text NOT NULL DEFAULT '',
    slug text NOT NULL DEFAULT '',
    status text NOT NULL DEFAULT 'active',
    synced_at timestamptz,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL
);

CREATE TABLE IF NOT EXISTS axis_tenants (
    id uuid PRIMARY KEY,
    org_id uuid NOT NULL REFERENCES axis_orgs(id),
    product_surface text NOT NULL,
    status text NOT NULL DEFAULT 'active',
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    UNIQUE (org_id, product_surface)
);

CREATE TABLE IF NOT EXISTS axis_tenant_members (
    tenant_id uuid NOT NULL REFERENCES axis_tenants(id) ON DELETE CASCADE,
    user_id uuid NOT NULL REFERENCES axis_users(id) ON DELETE CASCADE,
    role text NOT NULL DEFAULT 'member',
    status text NOT NULL DEFAULT 'active',
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, user_id)
);

CREATE INDEX IF NOT EXISTS idx_axis_tenants_org_product
    ON axis_tenants (org_id, product_surface);

CREATE INDEX IF NOT EXISTS idx_axis_tenant_members_user
    ON axis_tenant_members (user_id, tenant_id);

CREATE UNIQUE INDEX IF NOT EXISTS idx_axis_users_email_lower_unique
    ON axis_users (lower(email))
    WHERE email <> '';

CREATE UNIQUE INDEX IF NOT EXISTS idx_axis_users_provider_user_unique
    ON axis_users (provider, provider_user_id)
    WHERE provider_user_id <> '';

CREATE UNIQUE INDEX IF NOT EXISTS idx_axis_orgs_provider_org_unique
    ON axis_orgs (provider, provider_org_id)
    WHERE provider_org_id <> '';

CREATE TABLE IF NOT EXISTS axis_user_invitations (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL REFERENCES axis_tenants(id) ON DELETE CASCADE,
    org_id uuid NOT NULL REFERENCES axis_orgs(id) ON DELETE CASCADE,
    provider text NOT NULL DEFAULT 'dev',
    provider_invitation_id text NOT NULL DEFAULT '',
    email text NOT NULL,
    role text NOT NULL DEFAULT 'member',
    status text NOT NULL DEFAULT 'pending',
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    archived_at timestamptz,
    trashed_at timestamptz,
    purge_after timestamptz
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_axis_user_invitations_pending_email
    ON axis_user_invitations (tenant_id, lower(email))
    WHERE status = 'pending' AND archived_at IS NULL AND trashed_at IS NULL;
