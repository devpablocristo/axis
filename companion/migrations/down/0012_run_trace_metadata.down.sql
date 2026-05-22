ALTER TABLE companion_run_traces
    DROP COLUMN IF EXISTS model,
    DROP COLUMN IF EXISTS prompt_version;
