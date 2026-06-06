DROP INDEX IF EXISTS idx_companion_jobs_org_product_kind;

ALTER TABLE companion_jobs
    DROP COLUMN IF EXISTS product_surface;
