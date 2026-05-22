-- Módulo memory: continuidad operativa del compañero
CREATE TABLE companion_memory_entries (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    kind         VARCHAR(64) NOT NULL CHECK (kind IN (
        'task_summary', 'task_facts', 'playbook_snippet', 'user_preference'
    )),
    scope_type   VARCHAR(16) NOT NULL CHECK (scope_type IN ('task', 'org', 'user')),
    scope_id     VARCHAR(255) NOT NULL,
    key          VARCHAR(255) NOT NULL,
    payload_json JSONB NOT NULL DEFAULT '{}',
    content_text TEXT NOT NULL DEFAULT '',
    version      INTEGER NOT NULL DEFAULT 1,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at   TIMESTAMPTZ
);

CREATE INDEX idx_memory_scope_kind ON companion_memory_entries (scope_type, scope_id, kind);
CREATE INDEX idx_memory_expires ON companion_memory_entries (expires_at) WHERE expires_at IS NOT NULL;
CREATE UNIQUE INDEX idx_memory_scope_key ON companion_memory_entries (scope_type, scope_id, kind, key);

-- Módulo connectors: ejecución gobernada en sistemas externos
CREATE TABLE companion_connectors (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name        VARCHAR(255) NOT NULL,
    kind        VARCHAR(64) NOT NULL,
    enabled     BOOLEAN NOT NULL DEFAULT true,
    config_json JSONB NOT NULL DEFAULT '{}',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX idx_connectors_kind ON companion_connectors (kind);

CREATE TABLE companion_connector_executions (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    connector_id      UUID NOT NULL REFERENCES companion_connectors(id),
    operation         VARCHAR(128) NOT NULL,
    status            VARCHAR(32) NOT NULL CHECK (status IN ('success', 'failure', 'partial')),
    external_ref      VARCHAR(255) NOT NULL DEFAULT '',
    payload           JSONB NOT NULL DEFAULT '{}',
    result_json       JSONB NOT NULL DEFAULT '{}',
    error_message     TEXT,
    retryable         BOOLEAN NOT NULL DEFAULT false,
    duration_ms       BIGINT NOT NULL DEFAULT 0,
    task_id           UUID,
    review_request_id UUID,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_executions_connector ON companion_connector_executions (connector_id, created_at DESC);
CREATE INDEX idx_executions_task ON companion_connector_executions (task_id) WHERE task_id IS NOT NULL;
CREATE INDEX idx_executions_review ON companion_connector_executions (review_request_id) WHERE review_request_id IS NOT NULL;
