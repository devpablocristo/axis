-- Snapshot persistido del estado conocido en Nexus para reconciliación y observabilidad.

CREATE TABLE companion_task_nexus_sync_state (
    task_id                UUID PRIMARY KEY REFERENCES companion_tasks (id) ON DELETE CASCADE,
    nexus_request_id      UUID NOT NULL,
    last_nexus_status     TEXT NOT NULL DEFAULT '',
    last_nexus_http_status INTEGER NOT NULL DEFAULT 0,
    last_checked_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_error             TEXT NOT NULL DEFAULT '',
    consecutive_failures   INTEGER NOT NULL DEFAULT 0,
    next_check_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at             TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at             TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT companion_task_nexus_sync_failures_check CHECK (consecutive_failures >= 0)
);

CREATE INDEX idx_companion_task_nexus_sync_next_check
    ON companion_task_nexus_sync_state (next_check_at ASC);

CREATE INDEX idx_companion_task_nexus_sync_request
    ON companion_task_nexus_sync_state (nexus_request_id);
