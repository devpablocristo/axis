-- Fase 4 (PR3): record the human decision on a proposal. Accept installs the
-- procedure as a memory (provenance=system) and pins the resulting memory id;
-- dismiss discards it. decided_by is the human actor — the golden rule is that
-- a procedure memory can ONLY be created through this human-accept path.
SET lock_timeout = '5s';
SET statement_timeout = '30s';

ALTER TABLE companion_learning_proposals
    ADD COLUMN IF NOT EXISTS decided_by text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS decided_at timestamptz,
    ADD COLUMN IF NOT EXISTS memory_id uuid;
