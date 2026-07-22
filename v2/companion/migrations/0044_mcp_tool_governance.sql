SET lock_timeout = '5s';
SET statement_timeout = '30s';

-- 0036 introduced this tenant-scoped key, but older databases could skip the
-- original guard when another schema contained a constraint with the same
-- name. Scope the repair to the exact relation before creating MCP FKs.
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'companion_assist_cases_tenant_id_unique'
          AND conrelid = 'companion_assist_cases'::regclass
    ) THEN
        ALTER TABLE companion_assist_cases
            ADD CONSTRAINT companion_assist_cases_tenant_id_unique UNIQUE (tenant_id, id);
    END IF;
END $$;

CREATE TABLE IF NOT EXISTS companion_mcp_policies (
    tenant_id text PRIMARY KEY,
    enabled boolean NOT NULL DEFAULT false,
    kill_switch boolean NOT NULL DEFAULT false,
    allowed_capabilities text[] NOT NULL DEFAULT '{}',
    denied_capabilities text[] NOT NULL DEFAULT '{}',
    capability_kill_switches jsonb NOT NULL DEFAULT '{}'::jsonb,
    max_risk_class text NOT NULL DEFAULT 'high'
        CHECK (max_risk_class IN ('low','medium','high','critical')),
    max_calls_per_minute integer NOT NULL DEFAULT 120
        CHECK (max_calls_per_minute > 0 AND max_calls_per_minute <= 100000),
    max_concurrency integer NOT NULL DEFAULT 10
        CHECK (max_concurrency > 0 AND max_concurrency <= 1000),
    product_rules jsonb NOT NULL DEFAULT '{}'::jsonb,
    job_role_rules jsonb NOT NULL DEFAULT '{}'::jsonb,
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    changed_by text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CHECK (jsonb_typeof(capability_kill_switches) = 'object'),
    CHECK (jsonb_typeof(product_rules) = 'object'),
    CHECK (jsonb_typeof(job_role_rules) = 'object')
);

CREATE TABLE IF NOT EXISTS companion_mcp_policy_audit (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id text NOT NULL,
    actor_id text NOT NULL,
    previous_version bigint NOT NULL,
    new_version bigint NOT NULL,
    previous_policy jsonb NOT NULL,
    new_policy jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS companion_mcp_policy_audit_tenant_idx
    ON companion_mcp_policy_audit (tenant_id, created_at DESC, id DESC);

CREATE TABLE IF NOT EXISTS companion_mcp_invocations (
    id uuid PRIMARY KEY,
    tenant_id text NOT NULL,
    actor_id text NOT NULL,
    actor_role text NOT NULL DEFAULT '',
    virployee_id uuid NOT NULL,
    subject_id uuid NOT NULL,
    case_id uuid NULL,
    assignment_id uuid NOT NULL,
    assignment_version bigint NOT NULL,
    method text NOT NULL CHECK (method IN ('tools/list','tools/call')),
    capability_key text NOT NULL DEFAULT '',
    capability_version text NOT NULL DEFAULT '',
    manifest_hash text NOT NULL DEFAULT '',
    policy_version bigint NOT NULL,
    context_hash text NOT NULL DEFAULT '',
    payload_hash text NOT NULL DEFAULT '',
    idempotency_hash text NOT NULL DEFAULT '',
    result_hash text NOT NULL DEFAULT '',
    status text NOT NULL CHECK (status IN ('running','succeeded','blocked','pending_approval','failed')),
    blocked_by text NOT NULL DEFAULT '',
    error_code text NOT NULL DEFAULT '',
    approval_id text NOT NULL DEFAULT '',
    binding_hash text NOT NULL DEFAULT '',
    decision_reason text NOT NULL DEFAULT '',
    duration_ms bigint NOT NULL DEFAULT 0 CHECK (duration_ms >= 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    completed_at timestamptz NULL,
    CONSTRAINT companion_mcp_invocations_virployee_fkey
        FOREIGN KEY (tenant_id, virployee_id) REFERENCES virployees (tenant_id, id),
    CONSTRAINT companion_mcp_invocations_subject_fkey
        FOREIGN KEY (tenant_id, subject_id) REFERENCES companion_work_subjects (tenant_id, id),
    CONSTRAINT companion_mcp_invocations_assignment_fkey
        FOREIGN KEY (tenant_id, assignment_id) REFERENCES companion_continuity_assignments (tenant_id, id),
    CONSTRAINT companion_mcp_invocations_case_fkey
        FOREIGN KEY (tenant_id, case_id) REFERENCES companion_assist_cases (tenant_id, id)
);

CREATE INDEX IF NOT EXISTS companion_mcp_invocations_tenant_recent_idx
    ON companion_mcp_invocations (tenant_id, created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS companion_mcp_invocations_virployee_recent_idx
    ON companion_mcp_invocations (tenant_id, virployee_id, created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS companion_mcp_invocations_running_idx
    ON companion_mcp_invocations (tenant_id, created_at)
    WHERE status = 'running';
CREATE UNIQUE INDEX IF NOT EXISTS companion_mcp_write_idempotency_idx
    ON companion_mcp_invocations (tenant_id, virployee_id, capability_key, idempotency_hash)
    WHERE method = 'tools/call' AND idempotency_hash <> '';

COMMENT ON TABLE companion_mcp_invocations IS
    'Metadata-only MCP audit. Arguments, results, patient names, documents and conversations are never persisted.';
