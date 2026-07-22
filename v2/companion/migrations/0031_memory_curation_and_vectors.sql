SET lock_timeout = '5s';
SET statement_timeout = '30s';

CREATE EXTENSION IF NOT EXISTS vector;

ALTER TABLE companion_memories
    ADD COLUMN trust_score real NOT NULL DEFAULT 0.90 CHECK (trust_score >= 0 AND trust_score <= 1),
    ADD COLUMN review_state text NOT NULL DEFAULT 'approved'
        CHECK (review_state IN ('approved', 'pending', 'quarantined', 'rejected')),
    ADD COLUMN review_reason text NOT NULL DEFAULT '',
    ADD COLUMN poisoning_flags text[] NOT NULL DEFAULT '{}',
    ADD COLUMN pii_flags text[] NOT NULL DEFAULT '{}',
    ADD COLUMN expires_at timestamptz,
    ADD COLUMN decay_at timestamptz,
    ADD COLUMN last_recalled_at timestamptz,
    ADD COLUMN recall_count bigint NOT NULL DEFAULT 0 CHECK (recall_count >= 0),
    ADD COLUMN reviewed_by text NOT NULL DEFAULT '',
    ADD COLUMN reviewed_at timestamptz,
    ADD COLUMN embedding vector(768),
    ADD COLUMN embedding_model text NOT NULL DEFAULT '',
    ADD COLUMN embedding_version text NOT NULL DEFAULT '',
    ADD COLUMN embedding_content_hash text NOT NULL DEFAULT '';

CREATE INDEX companion_memories_safe_recall_idx
    ON companion_memories (tenant_id, virployee_id, updated_at DESC, id DESC)
    WHERE lifecycle_state = 'active' AND review_state = 'approved' AND trust_score >= 0.60
      AND sensitivity = 'normal' AND cardinality(poisoning_flags) = 0
      AND review_reason <> 'conflicting_memory_requires_review';

CREATE INDEX companion_memories_embedding_idx
    ON companion_memories USING hnsw (embedding vector_cosine_ops)
    WHERE embedding IS NOT NULL;

CREATE INDEX companion_memories_decay_idx
    ON companion_memories (decay_at, id)
    WHERE lifecycle_state = 'active' AND decay_at IS NOT NULL;

ALTER TABLE companion_memory_audit DROP CONSTRAINT companion_memory_audit_action_check;
ALTER TABLE companion_memory_audit ADD CONSTRAINT companion_memory_audit_action_check
    CHECK (action IN (
        'create', 'update', 'archive', 'unarchive', 'trash', 'restore', 'purge',
        'review_approve', 'review_reject', 'decay', 'index'
    ));

COMMENT ON COLUMN companion_memories.review_reason IS
    'Machine-readable reason only; never raw memory content, PII, secrets, or model output.';
COMMENT ON COLUMN companion_memories.poisoning_flags IS
    'Detection categories only; never the matched raw text.';
COMMENT ON COLUMN companion_memories.pii_flags IS
    'Detection categories only; never extracted personal values.';
