ALTER TABLE companion_jobs
    ADD COLUMN IF NOT EXISTS product_surface TEXT NOT NULL DEFAULT 'companion';

CREATE INDEX IF NOT EXISTS idx_companion_jobs_org_product_kind
    ON companion_jobs (org_id, product_surface, kind, created_at DESC);
