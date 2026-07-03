CREATE EXTENSION IF NOT EXISTS pgcrypto;

DO $$
DECLARE
    axis_users_id_type text;
BEGIN
    SELECT data_type
    INTO axis_users_id_type
    FROM information_schema.columns
    WHERE table_schema = current_schema()
      AND table_name = 'axis_users'
      AND column_name = 'id';

    IF axis_users_id_type = 'text' THEN
        CREATE TABLE axis_users_next (
            id uuid PRIMARY KEY,
            provider text NOT NULL DEFAULT 'dev',
            provider_user_id text NOT NULL DEFAULT '',
            email text NOT NULL DEFAULT '',
            status text NOT NULL DEFAULT 'active',
            synced_at timestamptz,
            created_at timestamptz NOT NULL,
            updated_at timestamptz NOT NULL,
            archived_at timestamptz,
            trashed_at timestamptz,
            purge_after timestamptz
        );

        CREATE TABLE axis_orgs_next (
            id uuid PRIMARY KEY,
            provider text NOT NULL DEFAULT 'dev',
            provider_org_id text NOT NULL DEFAULT '',
            name text NOT NULL DEFAULT '',
            slug text NOT NULL DEFAULT '',
            status text NOT NULL DEFAULT 'active',
            synced_at timestamptz,
            created_at timestamptz NOT NULL,
            updated_at timestamptz NOT NULL,
            archived_at timestamptz,
            trashed_at timestamptz,
            purge_after timestamptz
        );

        INSERT INTO axis_users_next (
            id,
            provider,
            provider_user_id,
            email,
            status,
            synced_at,
            created_at,
            updated_at,
            archived_at,
            trashed_at,
            purge_after
        )
        SELECT
            gen_random_uuid(),
            'dev',
            id,
            email,
            status,
            updated_at,
            created_at,
            updated_at,
            archived_at,
            trashed_at,
            purge_after
        FROM axis_users;

        INSERT INTO axis_orgs_next (
            id,
            provider,
            provider_org_id,
            name,
            slug,
            status,
            synced_at,
            created_at,
            updated_at,
            archived_at,
            trashed_at,
            purge_after
        )
        SELECT
            gen_random_uuid(),
            'dev',
            id,
            name,
            lower(regexp_replace(name, '[^a-zA-Z0-9]+', '-', 'g')),
            status,
            updated_at,
            created_at,
            updated_at,
            archived_at,
            trashed_at,
            purge_after
        FROM axis_orgs;

        CREATE TABLE axis_tenants_next (
            id uuid PRIMARY KEY,
            org_id uuid NOT NULL REFERENCES axis_orgs_next(id),
            product_surface text NOT NULL,
            status text NOT NULL DEFAULT 'active',
            created_at timestamptz NOT NULL,
            updated_at timestamptz NOT NULL,
            archived_at timestamptz,
            trashed_at timestamptz,
            purge_after timestamptz,
            UNIQUE (org_id, product_surface)
        );

        INSERT INTO axis_tenants_next (
            id,
            org_id,
            product_surface,
            status,
            created_at,
            updated_at,
            archived_at,
            trashed_at,
            purge_after
        )
        SELECT
            t.id,
            o.id,
            t.product_surface,
            t.status,
            t.created_at,
            t.updated_at,
            t.archived_at,
            t.trashed_at,
            t.purge_after
        FROM axis_tenants t
        JOIN axis_orgs_next o ON o.provider = 'dev' AND o.provider_org_id = t.org_id;

        CREATE TABLE axis_tenant_members_next (
            tenant_id uuid NOT NULL REFERENCES axis_tenants_next(id) ON DELETE CASCADE,
            user_id uuid NOT NULL REFERENCES axis_users_next(id) ON DELETE CASCADE,
            role text NOT NULL DEFAULT 'member',
            status text NOT NULL DEFAULT 'active',
            created_at timestamptz NOT NULL,
            updated_at timestamptz NOT NULL,
            archived_at timestamptz,
            trashed_at timestamptz,
            purge_after timestamptz,
            PRIMARY KEY (tenant_id, user_id)
        );

        INSERT INTO axis_tenant_members_next (
            tenant_id,
            user_id,
            role,
            status,
            created_at,
            updated_at,
            archived_at,
            trashed_at,
            purge_after
        )
        SELECT
            m.tenant_id,
            u.id,
            m.role,
            m.status,
            m.created_at,
            m.updated_at,
            m.archived_at,
            m.trashed_at,
            m.purge_after
        FROM axis_tenant_members m
        JOIN axis_users_next u ON u.provider = 'dev' AND u.provider_user_id = m.user_id;

        DROP TABLE axis_tenant_members;
        DROP TABLE axis_tenants;
        DROP TABLE axis_orgs;
        DROP TABLE axis_users;

        ALTER TABLE axis_users_next RENAME TO axis_users;
        ALTER TABLE axis_orgs_next RENAME TO axis_orgs;
        ALTER TABLE axis_tenants_next RENAME TO axis_tenants;
        ALTER TABLE axis_tenant_members_next RENAME TO axis_tenant_members;
    END IF;
END $$;

ALTER TABLE axis_users
    ADD COLUMN IF NOT EXISTS provider text NOT NULL DEFAULT 'dev',
    ADD COLUMN IF NOT EXISTS provider_user_id text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS synced_at timestamptz;

ALTER TABLE axis_orgs
    ADD COLUMN IF NOT EXISTS provider text NOT NULL DEFAULT 'dev',
    ADD COLUMN IF NOT EXISTS provider_org_id text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS slug text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS synced_at timestamptz;

CREATE UNIQUE INDEX IF NOT EXISTS idx_axis_users_provider_user_unique
    ON axis_users (provider, provider_user_id)
    WHERE provider_user_id <> '';

CREATE UNIQUE INDEX IF NOT EXISTS idx_axis_orgs_provider_org_unique
    ON axis_orgs (provider, provider_org_id)
    WHERE provider_org_id <> '';

CREATE INDEX IF NOT EXISTS idx_axis_tenants_org_product
    ON axis_tenants (org_id, product_surface);

CREATE INDEX IF NOT EXISTS idx_axis_tenant_members_user
    ON axis_tenant_members (user_id, tenant_id);

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
