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

INSERT INTO axis_products (id, product_surface, name, status, created_at, updated_at)
SELECT (
        substr(md5('axis-product:' || t.product_surface), 1, 8) || '-' ||
        substr(md5('axis-product:' || t.product_surface), 9, 4) || '-4' ||
        substr(md5('axis-product:' || t.product_surface), 14, 3) || '-8' ||
        substr(md5('axis-product:' || t.product_surface), 18, 3) || '-' ||
        substr(md5('axis-product:' || t.product_surface), 21, 12)
    )::uuid,
    t.product_surface,
    initcap(replace(t.product_surface, '-', ' ')),
    'active',
    now(),
    now()
FROM (
    SELECT DISTINCT product_surface
    FROM axis_tenants
    WHERE product_surface <> ''
) t
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

CREATE INDEX IF NOT EXISTS idx_axis_products_purge_after
    ON axis_products (purge_after)
    WHERE purge_after IS NOT NULL;
