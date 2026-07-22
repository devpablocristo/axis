CREATE TABLE IF NOT EXISTS companion_memories (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id text NOT NULL,
    virployee_id uuid NOT NULL REFERENCES virployees(id) ON DELETE CASCADE,
    title text NOT NULL,
    content text NOT NULL,
    memory_type text NOT NULL CHECK (memory_type IN ('fact', 'preference', 'procedure', 'note')),
    sensitivity text NOT NULL DEFAULT 'normal' CHECK (sensitivity IN ('normal', 'sensitive')),
    provenance text NOT NULL CHECK (provenance IN ('human', 'system')),
    actor_id text NOT NULL,
    source_reference text,
    content_hash text NOT NULL,
    version integer NOT NULL DEFAULT 1 CHECK (version > 0),
    lifecycle_state text NOT NULL DEFAULT 'active' CHECK (lifecycle_state IN ('active', 'archived', 'trash')),
    archived_at timestamptz,
    trashed_at timestamptz,
    purge_after timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, virployee_id, id)
);

DO $$
DECLARE
    boundary_column text;
BEGIN
    -- Some pre-migration environments already renamed the isolation boundary
    -- before this historical migration was registered. Keep the migration
    -- replay-safe for both upgrade paths; 0051 performs the canonical rename.
    SELECT CASE
        WHEN EXISTS (
            SELECT 1
            FROM information_schema.columns
            WHERE table_schema = current_schema()
              AND table_name = 'companion_memories'
              AND column_name = 'org_id'
        ) THEN 'org_id'
        ELSE 'tenant_id'
    END INTO boundary_column;

    EXECUTE format(
        'CREATE UNIQUE INDEX IF NOT EXISTS companion_memories_active_content_uq ON companion_memories (%I, virployee_id, content_hash) WHERE lifecycle_state = ''active''',
        boundary_column
    );
    EXECUTE format(
        'CREATE INDEX IF NOT EXISTS companion_memories_list_idx ON companion_memories (%I, virployee_id, lifecycle_state, updated_at DESC, id DESC)',
        boundary_column
    );
END $$;
CREATE INDEX IF NOT EXISTS companion_memories_search_idx
    ON companion_memories USING gin (to_tsvector('simple', title || ' ' || content));

CREATE TABLE IF NOT EXISTS companion_memory_audit (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id text NOT NULL,
    virployee_id uuid NOT NULL,
    memory_id uuid NOT NULL,
    action text NOT NULL CHECK (action IN ('create', 'update', 'archive', 'unarchive', 'trash', 'restore', 'purge')),
    actor_id text NOT NULL,
    previous_hash text,
    resulting_hash text,
    previous_version integer,
    resulting_version integer,
    metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now()
);
DO $$
DECLARE
    boundary_column text;
BEGIN
    SELECT CASE
        WHEN EXISTS (
            SELECT 1
            FROM information_schema.columns
            WHERE table_schema = current_schema()
              AND table_name = 'companion_memory_audit'
              AND column_name = 'org_id'
        ) THEN 'org_id'
        ELSE 'tenant_id'
    END INTO boundary_column;

    EXECUTE format(
        'CREATE INDEX IF NOT EXISTS companion_memory_audit_lookup_idx ON companion_memory_audit (%I, virployee_id, memory_id, created_at DESC)',
        boundary_column
    );
END $$;

ALTER TABLE companion_run_traces
    ADD COLUMN IF NOT EXISTS memory_references jsonb NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN IF NOT EXISTS memory_context_hash text NOT NULL DEFAULT '';
