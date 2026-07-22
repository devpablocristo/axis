SET lock_timeout = '5s';
SET statement_timeout = '30s';

-- Bind general-mode answers to the exact approved memory references supplied
-- to Runtime. Bodies remain in companion_memories; Assist stores metadata and
-- hashes only so execution can revalidate them without replaying retrieval.
ALTER TABLE companion_assist_runs
    ADD COLUMN IF NOT EXISTS memory_context_hash text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS memory_references jsonb NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN IF NOT EXISTS source_context jsonb NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN IF NOT EXISTS job_role_snapshot_hash text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS source_authorization_hash text NOT NULL DEFAULT '';

ALTER TABLE companion_assist_runs
    DROP CONSTRAINT IF EXISTS companion_assist_runs_memory_references_array_check;
ALTER TABLE companion_assist_runs
    ADD CONSTRAINT companion_assist_runs_memory_references_array_check
    CHECK (jsonb_typeof(memory_references) = 'array') NOT VALID;
ALTER TABLE companion_assist_runs
    VALIDATE CONSTRAINT companion_assist_runs_memory_references_array_check;

ALTER TABLE companion_assist_runs
    DROP CONSTRAINT IF EXISTS companion_assist_runs_source_context_array_check;
ALTER TABLE companion_assist_runs
    ADD CONSTRAINT companion_assist_runs_source_context_array_check
    CHECK (jsonb_typeof(source_context) = 'array') NOT VALID;
ALTER TABLE companion_assist_runs
    VALIDATE CONSTRAINT companion_assist_runs_source_context_array_check;

COMMENT ON COLUMN companion_assist_runs.memory_references IS
    'Metadata-only references for memories supplied to Runtime; never memory bodies.';
COMMENT ON COLUMN companion_assist_runs.source_context IS
    'All canonical source chunks supplied to Runtime; distinct from citations selected by the answer.';
COMMENT ON COLUMN companion_assist_runs.source_authorization_hash IS
    'Hash of the exact KB, document, and binding revisions authorizing the Runtime source set.';

-- Closed cases are historical records, not permanent locks. Only one live
-- case may exist for a subject/product/assist scope; closing it permits a new
-- treatment or procedure while preserving the old history.
ALTER TABLE companion_assist_cases
    DROP CONSTRAINT IF EXISTS companion_assist_cases_scope_unique;
CREATE UNIQUE INDEX IF NOT EXISTS companion_assist_cases_live_scope_uq
    ON companion_assist_cases (tenant_id, product_surface, assist_type, subject_id, entrypoint_virployee_id)
    WHERE status IN ('open', 'needs_human');
