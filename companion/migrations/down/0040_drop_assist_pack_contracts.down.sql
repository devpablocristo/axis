-- Reverse 0040: re-add the contract columns (nullable; original values are gone)
-- and restore the trigger function to its contract-aware form.

ALTER TABLE assist_packs
	ADD COLUMN IF NOT EXISTS input_contract TEXT NOT NULL DEFAULT '',
	ADD COLUMN IF NOT EXISTS output_contract TEXT NOT NULL DEFAULT '';

ALTER TABLE assist_pack_versions
	ADD COLUMN IF NOT EXISTS input_contract TEXT NOT NULL DEFAULT '',
	ADD COLUMN IF NOT EXISTS output_contract TEXT NOT NULL DEFAULT '';

CREATE OR REPLACE FUNCTION save_assist_pack_previous_version()
RETURNS TRIGGER AS $$
BEGIN
	IF OLD.owner_system IS DISTINCT FROM NEW.owner_system
		OR OLD.product_surface IS DISTINCT FROM NEW.product_surface
		OR OLD.assist_type IS DISTINCT FROM NEW.assist_type
		OR OLD.name IS DISTINCT FROM NEW.name
		OR OLD.description IS DISTINCT FROM NEW.description
		OR OLD.input_contract IS DISTINCT FROM NEW.input_contract
		OR OLD.output_contract IS DISTINCT FROM NEW.output_contract
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
			input_contract,
			output_contract,
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
			OLD.input_contract,
			OLD.output_contract,
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
