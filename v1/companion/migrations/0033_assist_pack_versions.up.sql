CREATE TABLE IF NOT EXISTS assist_pack_versions (
	id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
	assist_pack_id UUID NOT NULL REFERENCES assist_packs(id) ON DELETE CASCADE,
	org_id TEXT NOT NULL CHECK (btrim(org_id) <> ''),
	owner_system TEXT NOT NULL CHECK (btrim(owner_system) <> ''),
	product_surface TEXT NOT NULL CHECK (btrim(product_surface) <> ''),
	assist_type TEXT NOT NULL CHECK (btrim(assist_type) <> ''),
	version_label TEXT NOT NULL DEFAULT '',
	name TEXT NOT NULL CHECK (btrim(name) <> ''),
	description TEXT NOT NULL DEFAULT '',
	input_contract TEXT NOT NULL CHECK (btrim(input_contract) <> ''),
	output_contract TEXT NOT NULL CHECK (btrim(output_contract) <> ''),
	prompt_template TEXT NOT NULL CHECK (btrim(prompt_template) <> ''),
	model_policy_json JSONB NOT NULL DEFAULT '{}'::jsonb,
	enabled BOOLEAN NOT NULL DEFAULT true,
	archived_at TIMESTAMPTZ,
	original_created_at TIMESTAMPTZ NOT NULL,
	original_updated_at TIMESTAMPTZ NOT NULL,
	saved_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_assist_pack_versions_scope
	ON assist_pack_versions (org_id, owner_system, product_surface, assist_type, saved_at DESC);

CREATE INDEX IF NOT EXISTS idx_assist_pack_versions_pack
	ON assist_pack_versions (assist_pack_id, saved_at DESC);

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

DROP TRIGGER IF EXISTS trg_assist_pack_previous_version ON assist_packs;

CREATE TRIGGER trg_assist_pack_previous_version
BEFORE UPDATE ON assist_packs
FOR EACH ROW
EXECUTE FUNCTION save_assist_pack_previous_version();
