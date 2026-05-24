ALTER TABLE companion_run_traces
    DROP COLUMN IF EXISTS usage_json;

DROP TABLE IF EXISTS companion_runtime_usage_monthly;
DROP TABLE IF EXISTS companion_tenant_runtime_policies;
