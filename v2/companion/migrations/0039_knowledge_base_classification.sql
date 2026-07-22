SET lock_timeout = '5s';
SET statement_timeout = '30s';

-- Existing catalogs are private by default: no pre-existing patient artifact
-- can become professional knowledge merely by adding a broad binding.
ALTER TABLE companion_knowledge_bases
    ADD COLUMN IF NOT EXISTS classification text NOT NULL DEFAULT 'private';

ALTER TABLE companion_knowledge_bases
    ADD CONSTRAINT companion_knowledge_bases_classification_check
    CHECK (classification IN ('professional','private')) NOT VALID;

ALTER TABLE companion_knowledge_bases
    VALIDATE CONSTRAINT companion_knowledge_bases_classification_check;

COMMENT ON COLUMN companion_knowledge_bases.classification IS
    'professional accepts only non-personal artifact subject professional and professional/virployee bindings; private accepts only exact subject/case bindings.';
