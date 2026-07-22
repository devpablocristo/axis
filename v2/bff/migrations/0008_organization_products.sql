SET lock_timeout = '5s';
SET statement_timeout = '30s';

-- Organizations are the sole ownership and membership boundary. Products are
-- concrete children of an organization; there is no separate tenant entity.
ALTER TABLE axis_products RENAME TO axis_product_definitions;
ALTER TABLE axis_tenants RENAME TO axis_products;

ALTER TABLE axis_products ADD COLUMN name text NOT NULL DEFAULT '';
UPDATE axis_products p
SET name = d.name
FROM axis_product_definitions d
WHERE d.product_surface = p.product_surface;
UPDATE axis_products
SET name = initcap(replace(product_surface, '-', ' '))
WHERE name = '';

ALTER TABLE axis_products DROP CONSTRAINT IF EXISTS fk_axis_tenants_product_surface;
DROP TABLE axis_product_definitions;

CREATE TABLE axis_org_members (
    org_id uuid NOT NULL REFERENCES axis_orgs(id) ON DELETE CASCADE,
    user_id uuid NOT NULL REFERENCES axis_users(id) ON DELETE CASCADE,
    role text NOT NULL DEFAULT 'member',
    status text NOT NULL DEFAULT 'active',
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    archived_at timestamptz,
    trashed_at timestamptz,
    purge_after timestamptz,
    PRIMARY KEY (org_id, user_id)
);

INSERT INTO axis_org_members (org_id, user_id, role, status, created_at, updated_at, archived_at, trashed_at, purge_after)
SELECT p.org_id,
       m.user_id,
       CASE min(CASE m.role WHEN 'owner' THEN 1 WHEN 'admin' THEN 2 ELSE 3 END)
           WHEN 1 THEN 'owner' WHEN 2 THEN 'admin' ELSE 'member' END,
       CASE WHEN bool_or(m.status = 'active') THEN 'active' ELSE min(m.status) END,
       min(m.created_at),
       max(m.updated_at),
       min(m.archived_at),
       min(m.trashed_at),
       min(m.purge_after)
FROM axis_tenant_members m
JOIN axis_products p ON p.id = m.tenant_id
GROUP BY p.org_id, m.user_id;

DROP TABLE axis_tenant_members;
ALTER TABLE axis_user_invitations DROP COLUMN tenant_id;
CREATE UNIQUE INDEX idx_axis_user_invitations_pending_org_email
    ON axis_user_invitations (org_id, lower(email))
    WHERE status = 'pending' AND archived_at IS NULL AND trashed_at IS NULL;

ALTER INDEX IF EXISTS axis_tenants_pkey RENAME TO axis_products_pkey;
ALTER INDEX IF EXISTS axis_tenants_org_id_product_surface_key RENAME TO axis_products_org_id_product_surface_key;
