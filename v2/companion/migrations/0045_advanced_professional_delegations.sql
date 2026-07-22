SET lock_timeout = '5s';
SET statement_timeout = '30s';

ALTER TABLE professional_delegations ADD COLUMN IF NOT EXISTS granted_by text NOT NULL DEFAULT 'migration:legacy';
ALTER TABLE professional_delegations ADD COLUMN IF NOT EXISTS purpose text NOT NULL DEFAULT 'legacy professional authority';
ALTER TABLE professional_delegations ADD COLUMN IF NOT EXISTS product_scopes jsonb NOT NULL DEFAULT '["*"]'::jsonb;
ALTER TABLE professional_delegations ADD COLUMN IF NOT EXISTS resource_scopes jsonb NOT NULL DEFAULT '[]'::jsonb;
ALTER TABLE professional_delegations ADD COLUMN IF NOT EXISTS max_risk_class text NOT NULL DEFAULT 'critical';
ALTER TABLE professional_delegations ADD COLUMN IF NOT EXISTS reviewed_at timestamptz NULL;
ALTER TABLE professional_delegations ADD COLUMN IF NOT EXISTS reviewed_by text NOT NULL DEFAULT '';
ALTER TABLE professional_delegations ADD COLUMN IF NOT EXISTS review_note text NOT NULL DEFAULT '';

UPDATE professional_delegations
SET resource_scopes=jsonb_build_array(jsonb_build_object('resource_type','*','resource_id',principal_id))
WHERE resource_scopes='[]'::jsonb;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname='professional_delegations_max_risk_class_check'
          AND conrelid='professional_delegations'::regclass
    ) THEN
        ALTER TABLE professional_delegations ADD CONSTRAINT professional_delegations_max_risk_class_check
            CHECK (max_risk_class IN ('low','medium','high','critical'));
    END IF;
END $$;

COMMENT ON COLUMN professional_delegations.resource_scopes IS
    'Immutable resource allowlist. Legacy rows are pinned to the exact principal already linked to the delegation.';
