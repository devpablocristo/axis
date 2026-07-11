SET lock_timeout = '5s';
SET statement_timeout = '30s';

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

CREATE TABLE IF NOT EXISTS axis_products (
    id uuid PRIMARY KEY,
    product_surface text NOT NULL UNIQUE,
    name text NOT NULL,
    status text NOT NULL DEFAULT 'active',
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    archived_at timestamptz,
    trashed_at timestamptz,
    purge_after timestamptz
);

INSERT INTO axis_products (id, product_surface, name, status, created_at, updated_at)
VALUES
    ('00000000-0000-4000-8000-000000000001', 'axis', 'Axis', 'active', now(), now()),
    ('00000000-0000-4000-8000-000000000002', 'companion', 'Companion', 'active', now(), now()),
    ('00000000-0000-4000-8000-000000000003', 'medmory', 'Medmory', 'active', now(), now()),
    ('00000000-0000-4000-8000-000000000004', 'ponti', 'Ponti', 'active', now(), now()),
    ('00000000-0000-4000-8000-000000000005', 'pymes', 'Pymes', 'active', now(), now())
ON CONFLICT (product_surface) DO NOTHING;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'fk_axis_tenants_product_surface'
    ) THEN
        ALTER TABLE axis_tenants
            ADD CONSTRAINT fk_axis_tenants_product_surface
            FOREIGN KEY (product_surface)
            REFERENCES axis_products(product_surface);
    END IF;
END $$;

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
