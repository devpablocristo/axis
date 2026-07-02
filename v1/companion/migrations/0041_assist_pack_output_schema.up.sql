-- Add output_schema_json to assist_packs and its version-history table. Medmory
-- publishes a per-assist_type JSON Schema (subset OpenAPI) alongside the prompt;
-- Companion forwards it to the LLM provider as structured output (Gemini
-- responseSchema) so the model is forced to return that shape, instead of the
-- schema being described inside the prompt text and parsed best-effort.
-- Default '{}' = no schema = free-text behaviour (back-compat for existing packs).
-- The save_assist_pack_previous_version trigger must include the new column in
-- both its change-detection condition and its INSERT (mirror of 0040).

ALTER TABLE assist_packs
	ADD COLUMN IF NOT EXISTS output_schema_json JSONB NOT NULL DEFAULT '{}'::jsonb;

ALTER TABLE assist_pack_versions
	ADD COLUMN IF NOT EXISTS output_schema_json JSONB NOT NULL DEFAULT '{}'::jsonb;

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
		OR OLD.output_schema_json IS DISTINCT FROM NEW.output_schema_json
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
			output_schema_json,
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
			OLD.output_schema_json,
			OLD.enabled,
			OLD.archived_at,
			OLD.created_at,
			OLD.updated_at
		);
	END IF;
	RETURN NEW;
END;
$$ LANGUAGE plpgsql;
