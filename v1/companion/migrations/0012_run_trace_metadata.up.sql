-- Agrega metadata mínima para auditar qué prompt/modelo produjo cada run.

ALTER TABLE companion_run_traces
    ADD COLUMN IF NOT EXISTS prompt_version TEXT NOT NULL DEFAULT 'companion.system.v1',
    ADD COLUMN IF NOT EXISTS model TEXT NOT NULL DEFAULT '';
