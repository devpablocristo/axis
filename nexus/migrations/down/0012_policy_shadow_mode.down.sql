-- Reversa de 0012_policy_shadow_mode.up.sql
ALTER TABLE policies DROP COLUMN IF EXISTS shadow_hits;
ALTER TABLE policies DROP COLUMN IF EXISTS mode;
