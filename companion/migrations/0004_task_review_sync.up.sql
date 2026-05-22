-- Snapshot persistido del estado conocido en Review para reconciliación y observabilidad.

CREATE TABLE companion_task_review_sync_state (
    task_id                UUID PRIMARY KEY REFERENCES companion_tasks (id) ON DELETE CASCADE,
    review_request_id      UUID NOT NULL,
    last_review_status     TEXT NOT NULL DEFAULT '',
    last_review_http_status INTEGER NOT NULL DEFAULT 0,
    last_checked_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_error             TEXT NOT NULL DEFAULT '',
    consecutive_failures   INTEGER NOT NULL DEFAULT 0,
    next_check_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at             TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at             TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT companion_task_review_sync_failures_check CHECK (consecutive_failures >= 0)
);

CREATE INDEX idx_companion_task_review_sync_next_check
    ON companion_task_review_sync_state (next_check_at ASC);

CREATE INDEX idx_companion_task_review_sync_request
    ON companion_task_review_sync_state (review_request_id);
