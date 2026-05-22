-- Estado persistido de la última ejecución y su verificación.

CREATE TABLE companion_task_execution_state (
    task_id                UUID PRIMARY KEY REFERENCES companion_tasks (id) ON DELETE CASCADE,
    last_execution_id      UUID NOT NULL,
    last_execution_status  TEXT NOT NULL DEFAULT '',
    retryable              BOOLEAN NOT NULL DEFAULT false,
    retry_count            INTEGER NOT NULL DEFAULT 0,
    last_error             TEXT NOT NULL DEFAULT '',
    last_attempted_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    verification_result    JSONB NOT NULL DEFAULT '{}',
    created_at             TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at             TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT companion_task_execution_state_retry_count_check CHECK (retry_count >= 0)
);

CREATE INDEX idx_companion_task_execution_state_attempted_at
    ON companion_task_execution_state (last_attempted_at DESC);

CREATE INDEX idx_companion_task_execution_state_retryable
    ON companion_task_execution_state (retryable, updated_at DESC);
