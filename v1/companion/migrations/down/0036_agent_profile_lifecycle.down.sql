CREATE OR REPLACE FUNCTION save_agent_profile_previous_version()
RETURNS TRIGGER AS $$
BEGIN
	IF OLD.profile_id IS DISTINCT FROM NEW.profile_id
		OR OLD.family_id IS DISTINCT FROM NEW.family_id
		OR OLD.version_label IS DISTINCT FROM NEW.version_label
		OR OLD.name IS DISTINCT FROM NEW.name
		OR OLD.description IS DISTINCT FROM NEW.description
		OR OLD.system_prompt IS DISTINCT FROM NEW.system_prompt
		OR OLD.max_autonomy IS DISTINCT FROM NEW.max_autonomy
		OR OLD.allowed_tools IS DISTINCT FROM NEW.allowed_tools
		OR OLD.allowed_capabilities IS DISTINCT FROM NEW.allowed_capabilities
		OR OLD.memory_policy_json IS DISTINCT FROM NEW.memory_policy_json
		OR OLD.llm_config_json IS DISTINCT FROM NEW.llm_config_json
		OR OLD.enabled IS DISTINCT FROM NEW.enabled
		OR OLD.archived_at IS DISTINCT FROM NEW.archived_at THEN
		INSERT INTO agent_profile_versions (
			agent_profile_id,
			profile_id,
			family_id,
			version_label,
			name,
			description,
			system_prompt,
			max_autonomy,
			allowed_tools,
			allowed_capabilities,
			memory_policy_json,
			llm_config_json,
			enabled,
			archived_at,
			original_created_at,
			original_updated_at
		)
		VALUES (
			OLD.id,
			OLD.profile_id,
			OLD.family_id,
			OLD.version_label,
			OLD.name,
			OLD.description,
			OLD.system_prompt,
			OLD.max_autonomy,
			OLD.allowed_tools,
			OLD.allowed_capabilities,
			OLD.memory_policy_json,
			OLD.llm_config_json,
			OLD.enabled,
			OLD.archived_at,
			OLD.created_at,
			OLD.updated_at
		);
	END IF;
	RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP INDEX IF EXISTS idx_agent_profiles_trash;
DROP INDEX IF EXISTS idx_agent_profiles_active;

ALTER TABLE agent_profile_versions
	DROP COLUMN IF EXISTS trashed_at;

ALTER TABLE agent_profiles
	DROP COLUMN IF EXISTS trashed_at;

CREATE INDEX IF NOT EXISTS idx_agent_profiles_active
	ON agent_profiles (profile_id)
	WHERE enabled = true AND archived_at IS NULL;
