SET lock_timeout = '5s';
SET statement_timeout = '30s';

CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- Prompt bodies stay in Companion. Nexus only receives the stable identifiers
-- and hashes recorded by promotion authorization.
CREATE TABLE IF NOT EXISTS companion_prompts (
    id uuid PRIMARY KEY,
    org_id text NOT NULL,
    name text NOT NULL,
    description text NOT NULL DEFAULT '',
    legacy_target_type text NOT NULL DEFAULT '',
    legacy_target_id uuid NULL,
    created_by text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    archived_at timestamptz NULL,
    CONSTRAINT companion_prompts_name_check CHECK (btrim(name) <> ''),
    CONSTRAINT companion_prompts_legacy_target_check CHECK (
        (legacy_target_type = '' AND legacy_target_id IS NULL)
        OR (legacy_target_type IN ('job_role','profile_template','virployee') AND legacy_target_id IS NOT NULL)
    )
);

CREATE UNIQUE INDEX IF NOT EXISTS companion_prompts_legacy_target_unique
    ON companion_prompts (org_id, legacy_target_type, legacy_target_id)
    WHERE legacy_target_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS companion_prompts_org_recent_idx
    ON companion_prompts (org_id, created_at DESC, id DESC);

CREATE TABLE IF NOT EXISTS companion_prompt_versions (
    id uuid PRIMARY KEY,
    org_id text NOT NULL,
    prompt_id uuid NOT NULL REFERENCES companion_prompts(id),
    version bigint NOT NULL CHECK (version > 0),
    content text NOT NULL,
    content_hash text NOT NULL,
    created_by text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT companion_prompt_versions_content_check CHECK (btrim(content) <> ''),
    CONSTRAINT companion_prompt_versions_hash_check CHECK (content_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT companion_prompt_versions_number_unique UNIQUE (org_id, prompt_id, version),
    CONSTRAINT companion_prompt_versions_id_org_unique UNIQUE (org_id, id)
);

CREATE INDEX IF NOT EXISTS companion_prompt_versions_prompt_recent_idx
    ON companion_prompt_versions (org_id, prompt_id, version DESC);

CREATE OR REPLACE FUNCTION companion_reject_immutable_artifact_change()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION '% is immutable', TG_TABLE_NAME USING ERRCODE = '55000';
END;
$$;

DROP TRIGGER IF EXISTS companion_prompt_versions_immutable ON companion_prompt_versions;
CREATE TRIGGER companion_prompt_versions_immutable
    BEFORE UPDATE OR DELETE ON companion_prompt_versions
    FOR EACH ROW EXECUTE FUNCTION companion_reject_immutable_artifact_change();

CREATE TABLE IF NOT EXISTS companion_prompt_simulations (
    id uuid PRIMARY KEY,
    org_id text NOT NULL,
    prompt_version_id uuid NOT NULL,
    content_hash text NOT NULL,
    result_hash text NOT NULL,
    passed boolean NOT NULL,
    findings jsonb NOT NULL DEFAULT '[]'::jsonb,
    created_by text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT companion_prompt_simulations_version_fkey
        FOREIGN KEY (org_id, prompt_version_id)
        REFERENCES companion_prompt_versions (org_id, id),
    CONSTRAINT companion_prompt_simulations_hash_check
        CHECK (content_hash ~ '^[0-9a-f]{64}$' AND result_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT companion_prompt_simulations_findings_check CHECK (jsonb_typeof(findings) = 'array')
);

CREATE TABLE IF NOT EXISTS companion_evaluation_suites (
    id uuid PRIMARY KEY,
    org_id text NOT NULL,
    name text NOT NULL,
    description text NOT NULL DEFAULT '',
    artifact_type text NOT NULL,
    created_by text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    archived_at timestamptz NULL,
    CONSTRAINT companion_evaluation_suites_name_check CHECK (btrim(name) <> ''),
    CONSTRAINT companion_evaluation_suites_artifact_check
        CHECK (artifact_type IN ('prompt_version','capability_manifest','virployee_snapshot')),
    CONSTRAINT companion_evaluation_suites_id_org_unique UNIQUE (org_id, id)
);

CREATE TABLE IF NOT EXISTS companion_evaluation_suite_versions (
    id uuid PRIMARY KEY,
    org_id text NOT NULL,
    suite_id uuid NOT NULL,
    version bigint NOT NULL CHECK (version > 0),
    dataset jsonb NOT NULL,
    thresholds jsonb NOT NULL DEFAULT '{}'::jsonb,
    suite_hash text NOT NULL,
    created_by text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT companion_evaluation_suite_versions_suite_fkey
        FOREIGN KEY (org_id, suite_id)
        REFERENCES companion_evaluation_suites (org_id, id),
    CONSTRAINT companion_evaluation_suite_versions_dataset_check CHECK (jsonb_typeof(dataset) = 'array'),
    CONSTRAINT companion_evaluation_suite_versions_thresholds_check CHECK (jsonb_typeof(thresholds) = 'object'),
    CONSTRAINT companion_evaluation_suite_versions_hash_check CHECK (suite_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT companion_evaluation_suite_versions_number_unique UNIQUE (org_id, suite_id, version),
    CONSTRAINT companion_evaluation_suite_versions_id_org_unique UNIQUE (org_id, id)
);

DROP TRIGGER IF EXISTS companion_evaluation_suite_versions_immutable
    ON companion_evaluation_suite_versions;
CREATE TRIGGER companion_evaluation_suite_versions_immutable
    BEFORE UPDATE OR DELETE ON companion_evaluation_suite_versions
    FOR EACH ROW EXECUTE FUNCTION companion_reject_immutable_artifact_change();

CREATE TABLE IF NOT EXISTS companion_evaluation_runs (
    id uuid PRIMARY KEY,
    org_id text NOT NULL,
    suite_version_id uuid NOT NULL,
    artifact_type text NOT NULL,
    artifact_ref text NOT NULL,
    artifact_hash text NOT NULL,
    product_id text NOT NULL DEFAULT '',
    snapshot_hash text NOT NULL,
    status text NOT NULL,
    passed boolean NOT NULL DEFAULT false,
    metrics jsonb NOT NULL DEFAULT '{}'::jsonb,
    report_hash text NOT NULL DEFAULT '',
    created_by text NOT NULL,
    started_at timestamptz NOT NULL DEFAULT now(),
    completed_at timestamptz NULL,
    CONSTRAINT companion_evaluation_runs_suite_version_fkey
        FOREIGN KEY (org_id, suite_version_id)
        REFERENCES companion_evaluation_suite_versions (org_id, id),
    CONSTRAINT companion_evaluation_runs_artifact_check
        CHECK (artifact_type IN ('prompt_version','capability_manifest','virployee_snapshot')),
    CONSTRAINT companion_evaluation_runs_status_check CHECK (status IN ('running','passed','failed')),
    CONSTRAINT companion_evaluation_runs_artifact_hash_check CHECK (artifact_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT companion_evaluation_runs_snapshot_hash_check CHECK (snapshot_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT companion_evaluation_runs_report_hash_check CHECK (report_hash = '' OR report_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT companion_evaluation_runs_metrics_check CHECK (jsonb_typeof(metrics) = 'object'),
    CONSTRAINT companion_evaluation_runs_id_org_unique UNIQUE (org_id, id)
);

CREATE INDEX IF NOT EXISTS companion_evaluation_runs_fresh_idx
    ON companion_evaluation_runs (org_id, artifact_type, artifact_ref, artifact_hash, completed_at DESC)
    WHERE status = 'passed' AND passed;

CREATE TABLE IF NOT EXISTS companion_evaluation_results (
    id uuid PRIMARY KEY,
    org_id text NOT NULL,
    run_id uuid NOT NULL,
    case_key text NOT NULL,
    check_type text NOT NULL,
    passed boolean NOT NULL,
    result_hash text NOT NULL,
    error_code text NOT NULL DEFAULT '',
    metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT companion_evaluation_results_run_fkey
        FOREIGN KEY (org_id, run_id)
        REFERENCES companion_evaluation_runs (org_id, id) ON DELETE CASCADE,
    CONSTRAINT companion_evaluation_results_hash_check CHECK (result_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT companion_evaluation_results_metadata_check CHECK (jsonb_typeof(metadata) = 'object'),
    CONSTRAINT companion_evaluation_results_case_unique UNIQUE (org_id, run_id, case_key)
);

CREATE TABLE IF NOT EXISTS companion_prompt_bindings (
    id uuid PRIMARY KEY,
    org_id text NOT NULL,
    target_type text NOT NULL,
    target_id uuid NOT NULL,
    product_id text NOT NULL DEFAULT '',
    prompt_version_id uuid NOT NULL,
    revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
    evaluation_run_id uuid NULL,
    authorization_hash text NOT NULL DEFAULT '',
    promoted_by text NOT NULL,
    promoted_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT companion_prompt_bindings_target_check
        CHECK (target_type IN ('job_role','profile_template','virployee')),
    CONSTRAINT companion_prompt_bindings_version_fkey
        FOREIGN KEY (org_id, prompt_version_id)
        REFERENCES companion_prompt_versions (org_id, id),
    CONSTRAINT companion_prompt_bindings_evaluation_fkey
        FOREIGN KEY (org_id, evaluation_run_id)
        REFERENCES companion_evaluation_runs (org_id, id),
    CONSTRAINT companion_prompt_bindings_scope_unique UNIQUE (org_id, target_type, target_id, product_id),
    CONSTRAINT companion_prompt_bindings_id_org_unique UNIQUE (org_id, id)
);

CREATE INDEX IF NOT EXISTS companion_prompt_bindings_resolution_idx
    ON companion_prompt_bindings (org_id, target_type, target_id, product_id);

CREATE TABLE IF NOT EXISTS companion_prompt_binding_events (
    id uuid PRIMARY KEY,
    org_id text NOT NULL,
    binding_id uuid NOT NULL,
    action text NOT NULL,
    previous_version_id uuid NULL,
    new_version_id uuid NOT NULL,
    product_id text NOT NULL DEFAULT '',
    evaluation_run_id uuid NULL,
    authorization_hash text NOT NULL,
    actor_id text NOT NULL,
    binding_revision bigint NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT companion_prompt_binding_events_binding_fkey
        FOREIGN KEY (org_id, binding_id)
        REFERENCES companion_prompt_bindings (org_id, id),
    CONSTRAINT companion_prompt_binding_events_action_check CHECK (action IN ('promote','rollback','legacy_backfill')),
    CONSTRAINT companion_prompt_binding_events_authorization_hash_check
        CHECK (authorization_hash = '' OR authorization_hash ~ '^[0-9a-f]{64}$')
);

ALTER TABLE companion_assist_runs
    ADD COLUMN IF NOT EXISTS product_id text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS prompt_bundle_hash text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS prompt_versions jsonb NOT NULL DEFAULT '[]'::jsonb;

ALTER TABLE companion_assist_runs
    DROP CONSTRAINT IF EXISTS companion_assist_runs_prompt_bundle_hash_check;
ALTER TABLE companion_assist_runs
    ADD CONSTRAINT companion_assist_runs_prompt_bundle_hash_check
        CHECK (prompt_bundle_hash = '' OR prompt_bundle_hash ~ '^[0-9a-f]{64}$') NOT VALID;
ALTER TABLE companion_assist_runs
    VALIDATE CONSTRAINT companion_assist_runs_prompt_bundle_hash_check;

-- Existing profile prompts remain behaviorally identical and are explicitly
-- marked as legacy/evaluation_unknown by their NULL evaluation_run_id.
INSERT INTO companion_prompts (
    id, org_id, name, description, legacy_target_type, legacy_target_id, created_by, created_at
)
SELECT gen_random_uuid(), pt.org_id, pt.name || ' profile prompt', pt.description,
       'profile_template', pt.id, 'migration:0055', pt.created_at
FROM profile_templates pt
WHERE btrim(pt.system_prompt) <> ''
ON CONFLICT (org_id, legacy_target_type, legacy_target_id)
    WHERE legacy_target_id IS NOT NULL DO NOTHING;

INSERT INTO companion_prompt_versions (
    id, org_id, prompt_id, version, content, content_hash, created_by, created_at
)
SELECT gen_random_uuid(), p.org_id, p.id, 1, pt.system_prompt,
       encode(digest(convert_to(pt.system_prompt, 'UTF8'), 'sha256'), 'hex'),
       'migration:0055', pt.created_at
FROM companion_prompts p
JOIN profile_templates pt
  ON pt.org_id = p.org_id
 AND pt.id = p.legacy_target_id
WHERE p.legacy_target_type = 'profile_template'
  AND NOT EXISTS (
      SELECT 1 FROM companion_prompt_versions pv
      WHERE pv.org_id = p.org_id AND pv.prompt_id = p.id
  );

INSERT INTO companion_prompt_bindings (
    id, org_id, target_type, target_id, product_id, prompt_version_id,
    revision, evaluation_run_id, authorization_hash, promoted_by, promoted_at
)
SELECT gen_random_uuid(), p.org_id, 'profile_template', p.legacy_target_id, '',
       pv.id, 1, NULL, '', 'migration:0055', pv.created_at
FROM companion_prompts p
JOIN companion_prompt_versions pv
  ON pv.org_id = p.org_id
 AND pv.prompt_id = p.id
 AND pv.version = 1
WHERE p.legacy_target_type = 'profile_template'
ON CONFLICT (org_id, target_type, target_id, product_id) DO NOTHING;

INSERT INTO companion_prompt_binding_events (
    id, org_id, binding_id, action, previous_version_id, new_version_id,
    product_id, evaluation_run_id, authorization_hash, actor_id, binding_revision, created_at
)
SELECT gen_random_uuid(), b.org_id, b.id, 'legacy_backfill', NULL, b.prompt_version_id,
       b.product_id, NULL, '', 'migration:0055', b.revision, b.promoted_at
FROM companion_prompt_bindings b
WHERE b.promoted_by = 'migration:0055'
  AND NOT EXISTS (
      SELECT 1 FROM companion_prompt_binding_events e
      WHERE e.org_id = b.org_id AND e.binding_id = b.id
  );

COMMENT ON TABLE companion_prompt_versions IS
    'Immutable Companion-owned prompt content. Prompt bodies are never sent to Nexus.';
COMMENT ON TABLE companion_evaluation_runs IS
    'Synthetic, no-effect evaluation attestation bound to exact artifact and snapshot hashes.';
