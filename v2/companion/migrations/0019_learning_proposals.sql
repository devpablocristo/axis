-- Fase 4 (PR1): proposal inbox for procedural learning.
-- A proposal is a distilled procedure candidate awaiting human review. It is
-- NOT a memory: only an explicit human Accept (PR3) installs it as a
-- type=procedure memory. Nothing writes memories automatically.
SET lock_timeout = '5s';
SET statement_timeout = '30s';

CREATE TABLE companion_learning_proposals (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id text NOT NULL,
    virployee_id uuid NOT NULL REFERENCES virployees(id) ON DELETE CASCADE,
    capability_key text NOT NULL,
    title text NOT NULL,
    content text NOT NULL,
    content_hash text NOT NULL,
    evidence jsonb NOT NULL DEFAULT '{}'::jsonb,
    source_trace_ids jsonb NOT NULL DEFAULT '[]'::jsonb,
    status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'accepted', 'dismissed')),
    proposed_by text NOT NULL DEFAULT 'analyzer' CHECK (proposed_by IN ('analyzer', 'llm')),
    -- Typed watermark: successful executions seen at proposal time. After a
    -- dismissal the analyzer only re-proposes when the live count exceeds this
    -- value, so a human dismissal cannot be resurrected without new evidence.
    succeeded_watermark bigint NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT companion_learning_proposals_key_format_check CHECK (
        capability_key ~ '^[a-zñ]+\.[a-zñ]+\.[a-zñ]+$'
    )
);

-- One open proposal per (tenant, virployee, capability): the analyzer re-scans
-- idempotently and must never pile up duplicates for the same skill.
CREATE UNIQUE INDEX companion_learning_proposals_pending_uq
    ON companion_learning_proposals (tenant_id, virployee_id, capability_key)
    WHERE status = 'pending';

CREATE INDEX companion_learning_proposals_list_idx
    ON companion_learning_proposals (tenant_id, status, created_at DESC, id DESC);

-- Serves the analyzer's latest-proposal-per-pair lookup and per-virployee
-- listings without falling back to a tenant-wide filter-and-sort.
CREATE INDEX companion_learning_proposals_pair_idx
    ON companion_learning_proposals (tenant_id, virployee_id, capability_key, created_at DESC, id DESC);
