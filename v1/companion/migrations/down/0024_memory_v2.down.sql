DROP INDEX IF EXISTS idx_memory_summaries_scope_version;
DROP TABLE IF EXISTS companion_memory_summaries;

DROP INDEX IF EXISTS idx_memory_audit_org_created;
DROP INDEX IF EXISTS idx_memory_audit_memory_created;
DROP TABLE IF EXISTS companion_memory_audit;

DROP INDEX IF EXISTS idx_memory_v2_conflicts;
DROP INDEX IF EXISTS idx_memory_v2_namespace_type_status;

ALTER TABLE companion_memory_entries
    DROP CONSTRAINT IF EXISTS companion_memory_entries_status_check;

ALTER TABLE companion_memory_entries
    DROP COLUMN IF EXISTS poisoning_flags,
    DROP COLUMN IF EXISTS confidence_decay_at,
    DROP COLUMN IF EXISTS last_verified_at,
    DROP COLUMN IF EXISTS conflict_group_id,
    DROP COLUMN IF EXISTS superseded_by_id,
    DROP COLUMN IF EXISTS supersedes_id,
    DROP COLUMN IF EXISTS embedding_json,
    DROP COLUMN IF EXISTS embedding_model,
    DROP COLUMN IF EXISTS embedding_namespace,
    DROP COLUMN IF EXISTS trust_score,
    DROP COLUMN IF EXISTS source,
    DROP COLUMN IF EXISTS status;
