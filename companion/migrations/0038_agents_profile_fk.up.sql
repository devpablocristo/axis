-- F4.4: refuerza la integridad de companion_agents.profile_id con una FK física
-- hacia agent_profiles(profile_id), sin romper filas legacy.
--
-- Contexto:
--   * companion_agents.profile_id es TEXT NOT NULL DEFAULT '' y hasta ahora la
--     integridad referencial sólo se validaba en runtime (ProfileChecker en
--     SaveAgent). Eso deja agujeros: inserts directos, backfills o bugs pueden
--     dejar profile_id apuntando a un perfil inexistente.
--   * Hay filas legacy con profile_id = '' (agentes "sin perfil"). El runtime ya
--     trata '' y 'legacy.unprofiled' como "sin perfil" y los rechaza al ejecutar
--     (applyRuntimeAgent). Una FK dura ingenua rompería esas filas.
--
-- Enfoque elegido (opción b del plan): sembrar un perfil centinela
-- 'legacy.unprofiled' (deshabilitado + archivado, para que jamás se use en
-- runtime) y normalizar las filas con profile_id = '' hacia ese centinela. Así
-- todas las filas existentes satisfacen la FK y podemos agregarla sin
-- NOT VALID/excepciones. agent_profiles.profile_id ya tiene UNIQUE, requisito
-- para ser destino de una FK.
--
-- ON UPDATE CASCADE: si algún día se renombra un profile_id, los agentes siguen.
-- ON DELETE RESTRICT: impide borrar un perfil mientras haya agentes apuntándolo
-- (coherente con PurgeProfile, que ya bloquea el borrado con agentes activos).

INSERT INTO agent_profiles (
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
	archived_at
)
VALUES (
	'legacy.unprofiled',
	'legacy.unprofiled',
	'v0',
	'Legacy Unprofiled',
	'Sentinel profile so legacy companion_agents rows without a real profile satisfy the profile_id FK. Disabled and archived: the runtime rejects legacy.unprofiled, so it is never executable.',
	'Sentinel profile. Not for runtime use.',
	'A0',
	ARRAY[]::TEXT[],
	ARRAY[]::TEXT[],
	'{}'::jsonb,
	'{}'::jsonb,
	false,
	now()
)
ON CONFLICT (profile_id) DO NOTHING;

-- Normaliza filas legacy: '' (y NULL defensivo) pasan al centinela.
UPDATE companion_agents
SET profile_id = 'legacy.unprofiled'
WHERE profile_id IS NULL OR btrim(profile_id) = '';

-- Cambia el default de la columna para que nuevos inserts sin profile_id
-- explícito caigan en el centinela en vez de '' (que ya no satisface la FK).
ALTER TABLE companion_agents
	ALTER COLUMN profile_id SET DEFAULT 'legacy.unprofiled';

-- Agrega la FK. Todas las filas ya fueron normalizadas, así que valida limpio.
DO $$
BEGIN
	IF NOT EXISTS (
		SELECT 1 FROM pg_constraint WHERE conname = 'companion_agents_profile_id_fkey'
	) THEN
		ALTER TABLE companion_agents
			ADD CONSTRAINT companion_agents_profile_id_fkey
			FOREIGN KEY (profile_id)
			REFERENCES agent_profiles (profile_id)
			ON UPDATE CASCADE
			ON DELETE RESTRICT;
	END IF;
END $$;
