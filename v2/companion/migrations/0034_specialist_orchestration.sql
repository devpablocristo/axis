SET lock_timeout = '5s';
SET statement_timeout = '30s';

CREATE TABLE IF NOT EXISTS companion_assist_cases (
    id uuid PRIMARY KEY,
    tenant_id text NOT NULL,
    product_surface text NOT NULL,
    assist_type text NOT NULL,
    subject_id text NOT NULL,
    entrypoint_virployee_id uuid NOT NULL REFERENCES virployees(id),
    owner_virployee_id uuid NOT NULL REFERENCES virployees(id),
    status text NOT NULL DEFAULT 'open',
    version bigint NOT NULL DEFAULT 1,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    closed_at timestamptz NULL,
    CONSTRAINT companion_assist_cases_status_check
        CHECK (status IN ('open', 'needs_human', 'closed')),
    CONSTRAINT companion_assist_cases_scope_unique
        UNIQUE (tenant_id, product_surface, assist_type, subject_id, entrypoint_virployee_id)
);

CREATE INDEX IF NOT EXISTS idx_companion_assist_cases_owner
    ON companion_assist_cases (tenant_id, owner_virployee_id, status, updated_at DESC);

CREATE TABLE IF NOT EXISTS companion_orchestration_policies (
    id uuid PRIMARY KEY,
    tenant_id text NOT NULL,
    product_surface text NOT NULL,
    assist_type text NOT NULL,
    entrypoint_virployee_id uuid NOT NULL REFERENCES virployees(id),
    mode text NOT NULL DEFAULT 'disabled',
    selector_capability_id uuid NOT NULL REFERENCES capabilities(id),
    synthesis_capability_id uuid NOT NULL REFERENCES capabilities(id),
    output_schema jsonb NOT NULL DEFAULT '{}'::jsonb,
    max_specialists integer NOT NULL DEFAULT 3,
    max_depth integer NOT NULL DEFAULT 1,
    consultation_timeout_seconds integer NOT NULL DEFAULT 120,
    orchestration_timeout_seconds integer NOT NULL DEFAULT 300,
    version bigint NOT NULL DEFAULT 1,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT companion_orchestration_policies_mode_check
        CHECK (mode IN ('disabled', 'shadow', 'active')),
    CONSTRAINT companion_orchestration_policies_limits_check
        CHECK (max_specialists BETWEEN 1 AND 3 AND max_depth = 1
            AND consultation_timeout_seconds BETWEEN 1 AND 120
            AND orchestration_timeout_seconds BETWEEN consultation_timeout_seconds AND 300),
    CONSTRAINT companion_orchestration_policies_schema_check
        CHECK (jsonb_typeof(output_schema) = 'object'),
    CONSTRAINT companion_orchestration_policies_scope_unique
        UNIQUE (tenant_id, product_surface, assist_type, entrypoint_virployee_id)
);

CREATE TABLE IF NOT EXISTS companion_specialist_routes (
    id uuid PRIMARY KEY,
    tenant_id text NOT NULL,
    product_surface text NOT NULL,
    assist_type text NOT NULL,
    entrypoint_virployee_id uuid NOT NULL REFERENCES virployees(id),
    specialty_code text NOT NULL,
    target_virployee_id uuid NOT NULL REFERENCES virployees(id),
    capability_id uuid NOT NULL REFERENCES capabilities(id),
    requirement_mode text NOT NULL DEFAULT 'selector_allowed',
    enabled boolean NOT NULL DEFAULT true,
    version bigint NOT NULL DEFAULT 1,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT companion_specialist_routes_code_check
        CHECK (specialty_code ~ '^[a-z][a-z0-9]*(\.[a-z0-9][a-z0-9_-]*)+$'),
    CONSTRAINT companion_specialist_routes_requirement_check
        CHECK (requirement_mode IN ('advisory_only', 'selector_allowed', 'required')),
    CONSTRAINT companion_specialist_routes_no_self_check
        CHECK (entrypoint_virployee_id <> target_virployee_id),
    CONSTRAINT companion_specialist_routes_scope_unique
        UNIQUE (tenant_id, product_surface, assist_type, entrypoint_virployee_id, specialty_code)
);

ALTER TABLE companion_assist_runs
    ADD COLUMN IF NOT EXISTS case_id uuid NULL,
    ADD COLUMN IF NOT EXISTS responsible_virployee_id uuid NULL,
    ADD COLUMN IF NOT EXISTS orchestration_plan_id uuid NULL,
    ADD COLUMN IF NOT EXISTS orchestration_deadline_at timestamptz NULL,
    ADD COLUMN IF NOT EXISTS ownership_version bigint NOT NULL DEFAULT 1;

UPDATE companion_assist_runs
SET responsible_virployee_id = virployee_id
WHERE responsible_virployee_id IS NULL;

ALTER TABLE companion_assist_runs
    ADD CONSTRAINT companion_assist_runs_case_id_fkey
    FOREIGN KEY (case_id) REFERENCES companion_assist_cases(id) NOT VALID;
ALTER TABLE companion_assist_runs
    VALIDATE CONSTRAINT companion_assist_runs_case_id_fkey;

ALTER TABLE companion_assist_runs
    ADD CONSTRAINT companion_assist_runs_responsible_virployee_id_fkey
    FOREIGN KEY (responsible_virployee_id) REFERENCES virployees(id) NOT VALID;
ALTER TABLE companion_assist_runs
    VALIDATE CONSTRAINT companion_assist_runs_responsible_virployee_id_fkey;

ALTER TABLE companion_assist_runs
    DROP CONSTRAINT IF EXISTS companion_assist_runs_status_check;

ALTER TABLE companion_assist_runs
    ADD CONSTRAINT companion_assist_runs_status_check CHECK (
        status IN ('received', 'staging', 'extracting', 'indexing', 'planning',
                   'consulting', 'synthesizing', 'answering', 'done', 'failed', 'needs_human')
    ) NOT VALID;
ALTER TABLE companion_assist_runs VALIDATE CONSTRAINT companion_assist_runs_status_check;

CREATE TABLE IF NOT EXISTS companion_orchestration_plans (
    id uuid PRIMARY KEY,
    tenant_id text NOT NULL,
    case_id uuid NOT NULL REFERENCES companion_assist_cases(id),
    root_run_id uuid NOT NULL REFERENCES companion_assist_runs(id) ON DELETE CASCADE,
    policy_id uuid NOT NULL REFERENCES companion_orchestration_policies(id),
    policy_version bigint NOT NULL,
    output_schema jsonb NOT NULL DEFAULT '{}'::jsonb,
    responsible_virployee_id uuid NOT NULL REFERENCES virployees(id),
    decision text NOT NULL,
    status text NOT NULL,
    proposal jsonb NOT NULL DEFAULT '{}'::jsonb,
    plan_hash text NOT NULL,
    model text NOT NULL DEFAULT '',
    prompt_version text NOT NULL DEFAULT '',
    requested_count integer NOT NULL DEFAULT 0,
    completed_count integer NOT NULL DEFAULT 0,
    failed_count integer NOT NULL DEFAULT 0,
    deadline_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    completed_at timestamptz NULL,
    CONSTRAINT companion_orchestration_plans_decision_check
        CHECK (decision IN ('direct', 'consult', 'needs_human')),
    CONSTRAINT companion_orchestration_plans_status_check
        CHECK (status IN ('planned', 'consulting', 'ready', 'synthesizing', 'completed', 'failed', 'needs_human')),
    CONSTRAINT companion_orchestration_plans_hash_check
        CHECK (plan_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT companion_orchestration_plans_root_unique UNIQUE (tenant_id, root_run_id)
);

ALTER TABLE companion_assist_runs
    ADD CONSTRAINT companion_assist_runs_orchestration_plan_fkey
    FOREIGN KEY (orchestration_plan_id) REFERENCES companion_orchestration_plans(id) NOT VALID;
ALTER TABLE companion_assist_runs
    VALIDATE CONSTRAINT companion_assist_runs_orchestration_plan_fkey;

CREATE TABLE IF NOT EXISTS companion_specialist_consultations (
    id uuid PRIMARY KEY,
    tenant_id text NOT NULL,
    plan_id uuid NOT NULL REFERENCES companion_orchestration_plans(id) ON DELETE CASCADE,
    root_run_id uuid NOT NULL REFERENCES companion_assist_runs(id) ON DELETE CASCADE,
    case_id uuid NOT NULL REFERENCES companion_assist_cases(id),
    specialty_code text NOT NULL,
    target_virployee_id uuid NOT NULL REFERENCES virployees(id),
    capability_id uuid NOT NULL REFERENCES capabilities(id),
    requirement text NOT NULL,
    status text NOT NULL DEFAULT 'queued',
    focus_json jsonb NOT NULL DEFAULT '{}'::jsonb,
    focus_hash text NOT NULL,
    output jsonb NOT NULL DEFAULT '{}'::jsonb,
    output_hash text NOT NULL DEFAULT '',
    model text NOT NULL DEFAULT '',
    prompt_version text NOT NULL DEFAULT '',
    error_code text NOT NULL DEFAULT '',
    duration_ms bigint NOT NULL DEFAULT 0,
    started_at timestamptz NULL,
    completed_at timestamptz NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT companion_specialist_consultations_requirement_check
        CHECK (requirement IN ('required', 'advisory')),
    CONSTRAINT companion_specialist_consultations_status_check
        CHECK (status IN ('queued', 'running', 'completed', 'failed', 'cancelled', 'timed_out')),
    CONSTRAINT companion_specialist_consultations_focus_hash_check
        CHECK (focus_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT companion_specialist_consultations_output_hash_check
        CHECK (output_hash = '' OR output_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT companion_specialist_consultations_plan_specialty_unique
        UNIQUE (tenant_id, plan_id, specialty_code)
);

CREATE INDEX IF NOT EXISTS idx_companion_specialist_consultations_reconcile
    ON companion_specialist_consultations (tenant_id, plan_id, status, updated_at);

CREATE TABLE IF NOT EXISTS companion_handoffs (
    id uuid PRIMARY KEY,
    tenant_id text NOT NULL,
    case_id uuid NOT NULL REFERENCES companion_assist_cases(id),
    source_run_id uuid NULL REFERENCES companion_assist_runs(id),
    from_virployee_id uuid NOT NULL REFERENCES virployees(id),
    to_virployee_id uuid NOT NULL REFERENCES virployees(id),
    reason_code text NOT NULL,
    note text NOT NULL DEFAULT '',
    note_hash text NOT NULL DEFAULT '',
    status text NOT NULL DEFAULT 'pending',
    requested_by text NOT NULL,
    decided_by text NOT NULL DEFAULT '',
    decision_note text NOT NULL DEFAULT '',
    version bigint NOT NULL DEFAULT 1,
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    decided_at timestamptz NULL,
    CONSTRAINT companion_handoffs_status_check
        CHECK (status IN ('pending', 'accepted', 'rejected', 'cancelled', 'expired')),
    CONSTRAINT companion_handoffs_no_self_check CHECK (from_virployee_id <> to_virployee_id),
    CONSTRAINT companion_handoffs_note_hash_check
        CHECK (note_hash = '' OR note_hash ~ '^[0-9a-f]{64}$')
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_companion_handoffs_one_pending
    ON companion_handoffs (tenant_id, case_id) WHERE status = 'pending';
CREATE INDEX IF NOT EXISTS idx_companion_handoffs_inbox
    ON companion_handoffs (tenant_id, status, expires_at, created_at DESC);

CREATE TABLE IF NOT EXISTS companion_human_reviews (
    id uuid PRIMARY KEY,
    tenant_id text NOT NULL,
    case_id uuid NOT NULL REFERENCES companion_assist_cases(id),
    root_run_id uuid NOT NULL REFERENCES companion_assist_runs(id),
    handoff_id uuid NULL REFERENCES companion_handoffs(id),
    reason_code text NOT NULL,
    urgency text NOT NULL,
    status text NOT NULL DEFAULT 'pending',
    reviewer_user_id text NOT NULL DEFAULT '',
    outcome text NOT NULL DEFAULT '',
    note text NOT NULL DEFAULT '',
    note_hash text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    claimed_at timestamptz NULL,
    resolved_at timestamptz NULL,
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT companion_human_reviews_urgency_check
        CHECK (urgency IN ('routine', 'urgent')),
    CONSTRAINT companion_human_reviews_status_check
        CHECK (status IN ('pending', 'claimed', 'resolved')),
    CONSTRAINT companion_human_reviews_outcome_check
        CHECK (outcome IN ('', 'handled_externally', 'handoff_requested', 'dismissed')),
    CONSTRAINT companion_human_reviews_note_hash_check
        CHECK (note_hash = '' OR note_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT companion_human_reviews_root_unique UNIQUE (tenant_id, root_run_id)
);

CREATE INDEX IF NOT EXISTS idx_companion_human_reviews_inbox
    ON companion_human_reviews (tenant_id, status, urgency, created_at);
