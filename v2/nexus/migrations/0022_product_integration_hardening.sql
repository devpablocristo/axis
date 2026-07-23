SET lock_timeout = '5s';
SET statement_timeout = '30s';

-- Development stacks may already have applied an earlier revision of 0021.
-- Keep the forward migration safe while bringing those databases to the
-- immutable integration snapshot shape expected by the service.
ALTER TABLE nexus_product_integration_versions
    ADD COLUMN IF NOT EXISTS source_integration_id uuid,
    ADD COLUMN IF NOT EXISTS source_version_id uuid,
    ADD COLUMN IF NOT EXISTS source_revision bigint,
    ADD COLUMN IF NOT EXISTS contract_hash char(64);

UPDATE nexus_product_integration_versions
SET source_integration_id = COALESCE(source_integration_id, integration_id),
    source_version_id = COALESCE(source_version_id, id),
    source_revision = COALESCE(source_revision, revision),
    contract_hash = COALESCE(contract_hash, content_hash)
WHERE source_integration_id IS NULL
   OR source_version_id IS NULL
   OR source_revision IS NULL
   OR contract_hash IS NULL;

ALTER TABLE nexus_product_integration_versions
    ALTER COLUMN source_integration_id SET NOT NULL,
    ALTER COLUMN source_version_id SET NOT NULL,
    ALTER COLUMN source_revision SET NOT NULL,
    ALTER COLUMN contract_hash SET NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS uq_nexus_product_integration_source_version
    ON nexus_product_integration_versions (integration_id, source_version_id);
