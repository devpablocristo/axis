DROP INDEX IF EXISTS idx_companion_runtime_policy_audit_org_version;

DROP TABLE IF EXISTS companion_runtime_policy_audit;

ALTER TABLE companion_tenant_runtime_policies
    DROP COLUMN IF EXISTS control_plane_json;

ALTER TABLE companion_tenant_runtime_policies
    DROP COLUMN IF EXISTS settings_version;
