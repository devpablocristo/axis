-- Reverse of 0038_agents_profile_fk.up.sql.

-- 1. Quita la FK.
ALTER TABLE companion_agents
	DROP CONSTRAINT IF EXISTS companion_agents_profile_id_fkey;

-- 2. Restaura el default original de la columna ('').
ALTER TABLE companion_agents
	ALTER COLUMN profile_id SET DEFAULT '';

-- 3. Devuelve las filas centinela a su estado legacy (''), de modo que el
--    esquema quede idéntico al previo a la migración.
UPDATE companion_agents
SET profile_id = ''
WHERE profile_id = 'legacy.unprofiled';

-- 4. Elimina el perfil centinela. Tras el paso 3 ya no quedan agentes
--    apuntándolo, así que el borrado no viola integridad.
DELETE FROM agent_profiles
WHERE profile_id = 'legacy.unprofiled';
