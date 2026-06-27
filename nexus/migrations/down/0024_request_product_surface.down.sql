DROP INDEX IF EXISTS idx_requests_org_product;
ALTER TABLE requests DROP COLUMN IF EXISTS product_surface;
