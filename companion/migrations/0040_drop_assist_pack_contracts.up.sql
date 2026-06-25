-- Final step of removing input_contract/output_contract from the assist-pack
-- model (they were stored but never validated or read at runtime). 0039 relaxed
-- the constraints; Medmory has stopped sending/reading the fields. Now drop the
-- columns from assist_packs and its version-history table. The
-- save_assist_pack_previous_version trigger references OLD.input_contract/
-- output_contract, so it must be redefined WITHOUT them before the columns go.

CREATE OR REPLACE FUNCTION save_assist_pack_previous_version()
RETURNS TRIGGER AS $$
BEGIN
	IF OLD.owner_system IS DISTINCT FROM NEW.owner_system
		OR OLD.product_surface IS DISTINCT FROM NEW.product_surface
		OR OLD.assist_type IS DISTINCT FROM NEW.assist_type
		OR OLD.name IS DISTINCT FROM NEW.name
		OR OLD.description IS DISTINCT FROM NEW.description
		OR OLD.prompt_template IS DISTINCT FROM NEW.prompt_template
		OR OLD.model_policy_json IS DISTINCT FROM NEW.model_policy_json
		OR OLD.enabled IS DISTINCT FROM NEW.enabled
		OR OLD.archived_at IS DISTINCT FROM NEW.archived_at THEN
		INSERT INTO assist_pack_versions (
			assist_pack_id,
			org_id,
			owner_system,
			product_surface,
			assist_type,
			version_label,
			name,
			description,
			prompt_template,
			model_policy_json,
			enabled,
			archived_at,
			original_created_at,
			original_updated_at
		)
		VALUES (
			OLD.id,
			OLD.org_id,
			OLD.owner_system,
			OLD.product_surface,
			OLD.assist_type,
			COALESCE(OLD.model_policy_json->>'prompt_version', ''),
			OLD.name,
			OLD.description,
			OLD.prompt_template,
			OLD.model_policy_json,
			OLD.enabled,
			OLD.archived_at,
			OLD.created_at,
			OLD.updated_at
		);
	END IF;
	RETURN NEW;
END;
$$ LANGUAGE plpgsql;

ALTER TABLE assist_packs
	DROP COLUMN IF EXISTS input_contract,
	DROP COLUMN IF EXISTS output_contract;

ALTER TABLE assist_pack_versions
	DROP COLUMN IF EXISTS input_contract,
	DROP COLUMN IF EXISTS output_contract;
