-- Reversa de 0003_memory.up.sql
DROP INDEX IF EXISTS idx_memory_scope_key;
DROP INDEX IF EXISTS idx_memory_expires;
DROP INDEX IF EXISTS idx_memory_scope_kind;
DROP TABLE IF EXISTS companion_memory_entries;
