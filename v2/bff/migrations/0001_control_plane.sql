CREATE TABLE IF NOT EXISTS axis_users (
    id text PRIMARY KEY,
    email text NOT NULL DEFAULT '',
    name text NOT NULL DEFAULT '',
    status text NOT NULL DEFAULT 'active',
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL
);

CREATE TABLE IF NOT EXISTS axis_orgs (
    id text PRIMARY KEY,
    name text NOT NULL DEFAULT '',
    status text NOT NULL DEFAULT 'active',
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL
);

CREATE TABLE IF NOT EXISTS axis_tenants (
    id uuid PRIMARY KEY,
    org_id text NOT NULL REFERENCES axis_orgs(id),
    product_surface text NOT NULL,
    name text NOT NULL DEFAULT '',
    status text NOT NULL DEFAULT 'active',
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    UNIQUE (org_id, product_surface)
);

CREATE TABLE IF NOT EXISTS axis_tenant_members (
    tenant_id uuid NOT NULL REFERENCES axis_tenants(id) ON DELETE CASCADE,
    user_id text NOT NULL REFERENCES axis_users(id) ON DELETE CASCADE,
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
