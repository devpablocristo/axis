-- Planner durable v1.
--
-- companion_task_plans modela el plan cognitivo del empleado IA: objetivo,
-- estrategia, checkpoint actual, next_action y blockers. No reemplaza al
-- companion_task_execution_plans, que sigue modelando una ejecución concreta
-- de connector.

CREATE TABLE IF NOT EXISTS companion_task_plans (
    task_id          UUID PRIMARY KEY REFERENCES companion_tasks (id) ON DELETE CASCADE,
    org_id           TEXT NOT NULL CHECK (btrim(org_id) <> ''),
    objective        TEXT NOT NULL CHECK (btrim(objective) <> ''),
    status           TEXT NOT NULL DEFAULT 'active'
        CHECK (status IN ('draft', 'active', 'blocked', 'completed', 'failed', 'escalated')),
    strategy         TEXT NOT NULL DEFAULT '',
    assumptions_json JSONB NOT NULL DEFAULT '[]'::jsonb,
    constraints_json JSONB NOT NULL DEFAULT '[]'::jsonb,
    checkpoint_json  JSONB NOT NULL DEFAULT '{}'::jsonb,
    next_action      TEXT NOT NULL DEFAULT '',
    blocker          TEXT NOT NULL DEFAULT '',
    created_by       TEXT NOT NULL DEFAULT '',
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at     TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_companion_task_plans_org_status
    ON companion_task_plans (org_id, status, updated_at DESC);

CREATE TABLE IF NOT EXISTS companion_task_plan_steps (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id          UUID NOT NULL REFERENCES companion_task_plans (task_id) ON DELETE CASCADE,
    org_id           TEXT NOT NULL CHECK (btrim(org_id) <> ''),
    step_key         TEXT NOT NULL CHECK (btrim(step_key) <> ''),
    title            TEXT NOT NULL CHECK (btrim(title) <> ''),
    status           TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'ready', 'running', 'blocked', 'done', 'failed', 'skipped')),
    depends_on_json  JSONB NOT NULL DEFAULT '[]'::jsonb,
    tool_name        TEXT NOT NULL DEFAULT '',
    capability       TEXT NOT NULL DEFAULT '',
    expected_outcome TEXT NOT NULL DEFAULT '',
    postcondition    TEXT NOT NULL DEFAULT '',
    evidence_json    JSONB NOT NULL DEFAULT '{}'::jsonb,
    observation      TEXT NOT NULL DEFAULT '',
    blocker          TEXT NOT NULL DEFAULT '',
    error_message    TEXT NOT NULL DEFAULT '',
    attempt_count    INTEGER NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),
    sort_order       INTEGER NOT NULL DEFAULT 0,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at     TIMESTAMPTZ,
    UNIQUE (task_id, step_key)
);

CREATE INDEX IF NOT EXISTS idx_companion_task_plan_steps_task_order
    ON companion_task_plan_steps (task_id, sort_order, created_at);

CREATE INDEX IF NOT EXISTS idx_companion_task_plan_steps_org_status
    ON companion_task_plan_steps (org_id, status, updated_at DESC);
