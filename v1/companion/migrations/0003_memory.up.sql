-- Módulo memory: continuidad operativa del compañero
CREATE TABLE IF NOT EXISTS companion_memory_entries (
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

CREATE INDEX IF NOT EXISTS idx_memory_scope_kind ON companion_memory_entries (scope_type, scope_id, kind);
CREATE INDEX IF NOT EXISTS idx_memory_expires ON companion_memory_entries (expires_at) WHERE expires_at IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_memory_scope_key ON companion_memory_entries (scope_type, scope_id, kind, key);
