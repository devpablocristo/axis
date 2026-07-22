SET lock_timeout = '5s';
SET statement_timeout = '30s';

-- Existing virployees keep the legacy behavior.  The default is changed only
-- after the backfill so every newly-created virployee is source grounded.
ALTER TABLE virployees
    ADD COLUMN IF NOT EXISTS grounding_mode text NOT NULL DEFAULT 'general';
ALTER TABLE virployees DROP CONSTRAINT IF EXISTS virployees_grounding_mode_check;
ALTER TABLE virployees
    ADD CONSTRAINT virployees_grounding_mode_check
    CHECK (grounding_mode IN ('general', 'sources_only')) NOT VALID;
ALTER TABLE virployees VALIDATE CONSTRAINT virployees_grounding_mode_check;
ALTER TABLE virployees ALTER COLUMN grounding_mode SET DEFAULT 'sources_only';

-- Memories written before this migration are deliberately virployee-global.
-- A scoped recall may include that safe global layer, the exact subject layer,
-- and (when supplied) the exact case layer; it never scans sibling subjects.
ALTER TABLE companion_memories
    ADD COLUMN IF NOT EXISTS scope_type text NOT NULL DEFAULT 'virployee',
    ADD COLUMN IF NOT EXISTS subject_id text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS case_id uuid NULL;
ALTER TABLE companion_memories DROP CONSTRAINT IF EXISTS companion_memories_scope_check;
ALTER TABLE companion_memories
    ADD CONSTRAINT companion_memories_scope_check CHECK (
        (scope_type = 'virployee' AND subject_id = '' AND case_id IS NULL) OR
        (scope_type = 'subject' AND btrim(subject_id) <> '' AND case_id IS NULL) OR
        (scope_type = 'case' AND btrim(subject_id) <> '' AND case_id IS NOT NULL)
    ) NOT VALID;
ALTER TABLE companion_memories VALIDATE CONSTRAINT companion_memories_scope_check;

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'companion_assist_cases_tenant_id_unique') THEN
        ALTER TABLE companion_assist_cases
            ADD CONSTRAINT companion_assist_cases_tenant_id_unique UNIQUE (tenant_id, id);
    END IF;
END $$;

ALTER TABLE companion_memories
    ADD CONSTRAINT companion_memories_case_fkey
    FOREIGN KEY (tenant_id, case_id)
    REFERENCES companion_assist_cases (tenant_id, id) NOT VALID;
ALTER TABLE companion_memories VALIDATE CONSTRAINT companion_memories_case_fkey;

DROP INDEX IF EXISTS companion_memories_active_content_uq;
CREATE UNIQUE INDEX IF NOT EXISTS companion_memories_active_scoped_content_uq
    ON companion_memories (
        tenant_id, virployee_id, scope_type, subject_id,
        COALESCE(case_id, '00000000-0000-0000-0000-000000000000'::uuid), content_hash
    ) WHERE lifecycle_state = 'active';
CREATE INDEX IF NOT EXISTS companion_memories_scoped_recall_idx
    ON companion_memories (tenant_id, virployee_id, scope_type, subject_id, case_id, updated_at DESC, id DESC)
    WHERE lifecycle_state = 'active' AND review_state = 'approved' AND trust_score >= 0.60
      AND sensitivity = 'normal' AND cardinality(poisoning_flags) = 0
      AND review_reason <> 'conflicting_memory_requires_review';

ALTER TABLE companion_memory_audit
    ADD COLUMN IF NOT EXISTS scope_type text NOT NULL DEFAULT 'virployee',
    ADD COLUMN IF NOT EXISTS subject_id text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS case_id uuid NULL;

CREATE TABLE IF NOT EXISTS companion_knowledge_bases (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id text NOT NULL,
    name text NOT NULL CHECK (btrim(name) <> '' AND char_length(name) <= 200),
    description text NOT NULL DEFAULT '' CHECK (char_length(description) <= 2000),
    lifecycle_state text NOT NULL DEFAULT 'active' CHECK (lifecycle_state IN ('active','archived')),
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    archived_at timestamptz NULL,
    UNIQUE (tenant_id, id),
    UNIQUE (tenant_id, name)
);

-- A knowledge document is a durable, tenant-scoped reference to one document
-- already verified and indexed by the artifact pipeline.  Signed read URLs and
-- raw credentials are intentionally absent.
CREATE TABLE IF NOT EXISTS companion_knowledge_documents (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id text NOT NULL,
    knowledge_base_id uuid NOT NULL,
    title text NOT NULL CHECK (btrim(title) <> '' AND char_length(title) <= 300),
    artifact_virployee_id uuid NOT NULL,
    artifact_product_surface text NOT NULL CHECK (btrim(artifact_product_surface) <> ''),
    artifact_subject_id text NOT NULL CHECK (btrim(artifact_subject_id) <> ''),
    artifact_repository_generation text NOT NULL CHECK (btrim(artifact_repository_generation) <> ''),
    artifact_document_id text NOT NULL CHECK (btrim(artifact_document_id) <> ''),
    source_version text NOT NULL,
    source_sha256 text NOT NULL CHECK (source_sha256 ~ '^[0-9a-f]{64}$'),
    lifecycle_state text NOT NULL DEFAULT 'active' CHECK (lifecycle_state IN ('active','archived')),
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    archived_at timestamptz NULL,
    CONSTRAINT companion_knowledge_documents_base_fkey
        FOREIGN KEY (tenant_id, knowledge_base_id)
        REFERENCES companion_knowledge_bases(tenant_id, id) ON DELETE CASCADE,
    CONSTRAINT companion_knowledge_documents_artifact_fkey
        FOREIGN KEY (
            tenant_id, artifact_virployee_id, artifact_product_surface,
            artifact_subject_id, artifact_repository_generation, artifact_document_id
        ) REFERENCES companion_artifacts (
            tenant_id, virployee_id, product_surface,
            subject_id, repository_generation, document_id
        ),
    UNIQUE (tenant_id, knowledge_base_id, id),
    UNIQUE (
        tenant_id, knowledge_base_id, artifact_virployee_id,
        artifact_product_surface, artifact_subject_id,
        artifact_repository_generation, artifact_document_id
    )
);

CREATE INDEX IF NOT EXISTS companion_knowledge_documents_lookup_idx
    ON companion_knowledge_documents (tenant_id, knowledge_base_id, lifecycle_state, updated_at DESC, id);

-- Professional bindings use a Job Role and therefore apply to every active
-- virployee in that profession.  Private bindings always include the exact
-- virployee and, for subject/case data, the exact subject context.
CREATE TABLE IF NOT EXISTS companion_knowledge_bindings (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id text NOT NULL,
    knowledge_base_id uuid NOT NULL,
    scope_type text NOT NULL CHECK (scope_type IN ('professional','virployee','subject','case')),
    job_role_id uuid NULL,
    virployee_id uuid NULL,
    subject_id text NOT NULL DEFAULT '',
    case_id uuid NULL,
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT companion_knowledge_bindings_base_fkey
        FOREIGN KEY (tenant_id, knowledge_base_id)
        REFERENCES companion_knowledge_bases(tenant_id, id) ON DELETE CASCADE,
    CONSTRAINT companion_knowledge_bindings_job_role_fkey
        FOREIGN KEY (tenant_id, job_role_id) REFERENCES job_roles(tenant_id, id),
    CONSTRAINT companion_knowledge_bindings_virployee_fkey
        FOREIGN KEY (tenant_id, virployee_id) REFERENCES virployees(tenant_id, id),
    CONSTRAINT companion_knowledge_bindings_case_fkey
        FOREIGN KEY (tenant_id, case_id) REFERENCES companion_assist_cases(tenant_id, id),
    CONSTRAINT companion_knowledge_bindings_scope_check CHECK (
        (scope_type = 'professional' AND job_role_id IS NOT NULL AND virployee_id IS NULL AND subject_id = '' AND case_id IS NULL) OR
        (scope_type = 'virployee' AND job_role_id IS NULL AND virployee_id IS NOT NULL AND subject_id = '' AND case_id IS NULL) OR
        (scope_type = 'subject' AND job_role_id IS NULL AND virployee_id IS NOT NULL AND btrim(subject_id) <> '' AND case_id IS NULL) OR
        (scope_type = 'case' AND job_role_id IS NULL AND virployee_id IS NOT NULL AND btrim(subject_id) <> '' AND case_id IS NOT NULL)
    )
);

CREATE UNIQUE INDEX IF NOT EXISTS companion_knowledge_bindings_scope_uq
    ON companion_knowledge_bindings (
        tenant_id, knowledge_base_id, scope_type,
        COALESCE(job_role_id, '00000000-0000-0000-0000-000000000000'::uuid),
        COALESCE(virployee_id, '00000000-0000-0000-0000-000000000000'::uuid),
        subject_id,
        COALESCE(case_id, '00000000-0000-0000-0000-000000000000'::uuid)
    );
CREATE INDEX IF NOT EXISTS companion_knowledge_bindings_resolve_idx
    ON companion_knowledge_bindings (tenant_id, scope_type, job_role_id, virployee_id, subject_id, case_id, knowledge_base_id);

ALTER TABLE companion_assist_runs
    ADD COLUMN IF NOT EXISTS grounding_mode text NOT NULL DEFAULT 'general',
    ADD COLUMN IF NOT EXISTS answer_status text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS citations jsonb NOT NULL DEFAULT '[]'::jsonb;
ALTER TABLE companion_assist_runs DROP CONSTRAINT IF EXISTS companion_assist_runs_grounding_mode_check;
ALTER TABLE companion_assist_runs
    ADD CONSTRAINT companion_assist_runs_grounding_mode_check
    CHECK (grounding_mode IN ('general','sources_only')) NOT VALID;
ALTER TABLE companion_assist_runs VALIDATE CONSTRAINT companion_assist_runs_grounding_mode_check;
ALTER TABLE companion_assist_runs DROP CONSTRAINT IF EXISTS companion_assist_runs_answer_status_check;
ALTER TABLE companion_assist_runs
    ADD CONSTRAINT companion_assist_runs_answer_status_check
    CHECK (answer_status IN ('','answered','abstained','escalation_required')) NOT VALID;
ALTER TABLE companion_assist_runs VALIDATE CONSTRAINT companion_assist_runs_answer_status_check;

COMMENT ON TABLE companion_knowledge_documents IS
    'References only verified artifact-pipeline documents; never stores signed transport URLs.';
COMMENT ON COLUMN companion_assist_runs.citations IS
    'Validated source identifiers and locators only; never source bodies or signed URLs.';
