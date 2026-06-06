-- Capability platform phase 3: blocked state and import source metadata.

ALTER TABLE companion_capability_manifests
    ADD COLUMN IF NOT EXISTS source_uri TEXT NOT NULL DEFAULT '';

ALTER TABLE companion_capability_manifests
    DROP CONSTRAINT IF EXISTS companion_capability_manifests_status_check;

ALTER TABLE companion_capability_manifests
    ADD CONSTRAINT companion_capability_manifests_status_check
    CHECK (status IN ('draft', 'active', 'deprecated', 'blocked'));

ALTER TABLE companion_capability_manifests
    DROP CONSTRAINT IF EXISTS companion_capability_manifests_source_check;

ALTER TABLE companion_capability_manifests
    ADD CONSTRAINT companion_capability_manifests_source_check
    CHECK (source IN ('generated', 'imported', 'url'));
