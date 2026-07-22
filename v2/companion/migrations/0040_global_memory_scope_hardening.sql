SET lock_timeout = '5s';
SET statement_timeout = '30s';

-- Legacy global memories cannot be proven non-personal. Quarantine every
-- non-procedure item so patient facts/preferences/notes are never injected
-- into a sibling patient's context. An owner may recreate it with an explicit
-- subject/case scope after review.
UPDATE companion_memories
SET review_state = 'quarantined',
    review_reason = 'legacy_global_memory_requires_scope_review',
    updated_at = now()
WHERE scope_type = 'virployee'
  AND memory_type <> 'procedure'
  AND lifecycle_state = 'active'
  AND review_state <> 'quarantined';

ALTER TABLE companion_memories
    DROP CONSTRAINT IF EXISTS companion_memories_global_procedure_check;
ALTER TABLE companion_memories
    ADD CONSTRAINT companion_memories_global_procedure_check CHECK (
        scope_type <> 'virployee' OR memory_type = 'procedure' OR
        (review_state = 'quarantined' AND review_reason = 'legacy_global_memory_requires_scope_review')
    ) NOT VALID;

-- Existing rows were quarantined rather than rewritten or deleted, so the
-- constraint applies to all new/updated memories while preserving reviewable
-- legacy history.
COMMENT ON CONSTRAINT companion_memories_global_procedure_check ON companion_memories IS
    'Virployee-global memory is non-personal procedure knowledge only; patient facts/preferences/notes require subject or case scope.';
