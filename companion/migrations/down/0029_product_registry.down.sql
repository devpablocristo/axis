DROP INDEX IF EXISTS idx_security_eval_reports_org_product_time;
DROP INDEX IF EXISTS idx_companion_cost_events_org_product_time;
DROP INDEX IF EXISTS idx_companion_observability_org_product_time;

ALTER TABLE companion_security_eval_reports
    DROP COLUMN IF EXISTS product_surface;

ALTER TABLE companion_cost_events
    DROP COLUMN IF EXISTS product_surface;

ALTER TABLE companion_observability_events
    DROP COLUMN IF EXISTS product_surface;

DROP TABLE IF EXISTS companion_product_installations;
DROP TABLE IF EXISTS companion_products;
