SET lock_timeout = '5s';
SET statement_timeout = '30s';

ALTER TABLE companion_assist_runs
    ADD COLUMN IF NOT EXISTS product_id text NOT NULL DEFAULT '';

CREATE OR REPLACE FUNCTION companion_reject_immutable_artifact_change()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION '% is immutable', TG_TABLE_NAME USING ERRCODE = '55000';
END;
$$;

DROP TRIGGER IF EXISTS companion_prompt_versions_immutable ON companion_prompt_versions;
CREATE TRIGGER companion_prompt_versions_immutable
    BEFORE UPDATE OR DELETE ON companion_prompt_versions
    FOR EACH ROW EXECUTE FUNCTION companion_reject_immutable_artifact_change();

DROP TRIGGER IF EXISTS companion_evaluation_suite_versions_immutable
    ON companion_evaluation_suite_versions;
CREATE TRIGGER companion_evaluation_suite_versions_immutable
    BEFORE UPDATE OR DELETE ON companion_evaluation_suite_versions
    FOR EACH ROW EXECUTE FUNCTION companion_reject_immutable_artifact_change();
