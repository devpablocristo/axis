-- Companion: tareas, mensajes, acciones y artefactos

CREATE TABLE companion_tasks (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    title        TEXT NOT NULL,
    goal         TEXT NOT NULL DEFAULT '',
    status       TEXT NOT NULL DEFAULT 'new',
    priority     TEXT NOT NULL DEFAULT 'normal',
    created_by   TEXT NOT NULL DEFAULT '',
    assigned_to  TEXT NOT NULL DEFAULT '',
    channel      TEXT NOT NULL DEFAULT '',
    summary      TEXT NOT NULL DEFAULT '',
    context_json JSONB NOT NULL DEFAULT '{}',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    closed_at    TIMESTAMPTZ,
    CONSTRAINT companion_tasks_status_check CHECK (status IN (
        'new', 'investigating', 'proposing', 'waiting_for_input', 'waiting_for_approval',
        'executing', 'verifying', 'done', 'failed', 'escalated'
    )),
    CONSTRAINT companion_tasks_priority_check CHECK (priority IN ('low', 'normal', 'high', 'urgent'))
);

CREATE INDEX idx_companion_tasks_status ON companion_tasks (status, updated_at DESC);
CREATE INDEX idx_companion_tasks_created ON companion_tasks (created_at DESC);

CREATE TABLE companion_task_messages (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id     UUID NOT NULL REFERENCES companion_tasks (id) ON DELETE CASCADE,
    author_type TEXT NOT NULL DEFAULT 'user',
    author_id   TEXT NOT NULL DEFAULT '',
    body        TEXT NOT NULL,
    metadata    JSONB NOT NULL DEFAULT '{}',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_companion_task_messages_task ON companion_task_messages (task_id, created_at);

CREATE TABLE companion_task_actions (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id           UUID NOT NULL REFERENCES companion_tasks (id) ON DELETE CASCADE,
    action_type       TEXT NOT NULL,
    payload           JSONB NOT NULL DEFAULT '{}',
    review_request_id UUID,
    error_message     TEXT,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_companion_task_actions_task ON companion_task_actions (task_id, created_at);

CREATE TABLE companion_task_artifacts (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id    UUID NOT NULL REFERENCES companion_tasks (id) ON DELETE CASCADE,
    kind       TEXT NOT NULL DEFAULT '',
    uri        TEXT NOT NULL DEFAULT '',
    payload    JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_companion_task_artifacts_task ON companion_task_artifacts (task_id, created_at);
