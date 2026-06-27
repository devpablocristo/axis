-- 0024: per-product scoping foundation.
-- Tag governance requests with the product surface (a tenant = org x product),
-- so requests can be partitioned per product within an org. Additive and
-- backward-compatible: existing rows default to '' (no product => unscoped),
-- preserving current org-only behavior until products start tagging requests.
ALTER TABLE requests ADD COLUMN IF NOT EXISTS product_surface TEXT NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_requests_org_product ON requests (org_id, product_surface);
