DROP TRIGGER IF EXISTS trg_agent_profile_previous_version ON agent_profiles;
DROP FUNCTION IF EXISTS save_agent_profile_previous_version();
DROP TABLE IF EXISTS agent_profile_versions;
DROP INDEX IF EXISTS idx_agent_profiles_active;
DROP INDEX IF EXISTS idx_agent_profiles_family;
DROP TABLE IF EXISTS agent_profiles;
