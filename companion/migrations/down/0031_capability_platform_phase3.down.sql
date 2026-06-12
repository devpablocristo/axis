UPDATE companion_capability_manifests
SET status = 'draft'
WHERE status = 'blocked';

UPDATE companion_capability_manifests
SET source = 'imported'
WHERE source = 'url';

ALTER TABLE companion_capability_manifests
    DROP CONSTRAINT IF EXISTS companion_capability_manifests_source_check;

ALTER TABLE companion_capability_manifests
    ADD CONSTRAINT companion_capability_manifests_source_check
    CHECK (source IN ('generated', 'imported'));

ALTER TABLE companion_capability_manifests
    DROP CONSTRAINT IF EXISTS companion_capability_manifests_status_check;

ALTER TABLE companion_capability_manifests
    ADD CONSTRAINT companion_capability_manifests_status_check
    CHECK (status IN ('draft', 'active', 'deprecated'));

ALTER TABLE companion_capability_manifests
    DROP COLUMN IF EXISTS source_uri;
