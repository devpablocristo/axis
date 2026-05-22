-- Agrega org_id para multi-tenancy real.
-- NULL = global (aplica a todas las orgs).

ALTER TABLE requests ADD COLUMN org_id TEXT;
ALTER TABLE policies ADD COLUMN org_id TEXT;
ALTER TABLE delegations ADD COLUMN org_id TEXT;
ALTER TABLE approvals ADD COLUMN org_id TEXT;

-- Índices para filtrar por org
CREATE INDEX IF NOT EXISTS idx_requests_org_id ON requests(org_id) WHERE org_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_policies_org_id ON policies(org_id) WHERE org_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_delegations_org_id ON delegations(org_id) WHERE org_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_approvals_org_id ON approvals(org_id) WHERE org_id IS NOT NULL;
