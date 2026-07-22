SET lock_timeout = '5s';
SET statement_timeout = '30s';

ALTER TABLE action_types DROP CONSTRAINT IF EXISTS action_types_risk_class_check;
ALTER TABLE action_types ADD CONSTRAINT action_types_risk_class_check
    CHECK (risk_class IN ('low','medium','high','critical')) NOT VALID;
ALTER TABLE action_types VALIDATE CONSTRAINT action_types_risk_class_check;

ALTER TABLE approvals
    ADD COLUMN IF NOT EXISTS approval_kind text NOT NULL DEFAULT 'normal',
    ADD COLUMN IF NOT EXISTS quorum_required smallint NOT NULL DEFAULT 1,
    ADD COLUMN IF NOT EXISTS post_review_required boolean NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS reviewed_by text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS review_note text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS reviewed_at timestamptz;

ALTER TABLE approvals DROP CONSTRAINT IF EXISTS approvals_kind_check;
ALTER TABLE approvals ADD CONSTRAINT approvals_kind_check
    CHECK (approval_kind IN ('normal','break_glass')) NOT VALID;
ALTER TABLE approvals VALIDATE CONSTRAINT approvals_kind_check;
ALTER TABLE approvals DROP CONSTRAINT IF EXISTS approvals_quorum_check;
ALTER TABLE approvals ADD CONSTRAINT approvals_quorum_check
    CHECK (quorum_required BETWEEN 1 AND 10) NOT VALID;
ALTER TABLE approvals VALIDATE CONSTRAINT approvals_quorum_check;

CREATE TABLE IF NOT EXISTS approval_decisions (
    id uuid PRIMARY KEY,
    tenant_id text NOT NULL,
    approval_id uuid NOT NULL REFERENCES approvals(id) ON DELETE RESTRICT,
    actor_id text NOT NULL,
    actor_role text NOT NULL,
    decision text NOT NULL CHECK (decision IN ('approve','reject')),
    note text NOT NULL DEFAULT '',
    decided_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, approval_id, actor_id)
);

INSERT INTO approval_decisions (id,tenant_id,approval_id,actor_id,actor_role,decision,note,decided_at)
SELECT gen_random_uuid(), tenant_id, id, decided_by, 'legacy',
       CASE status WHEN 'approved' THEN 'approve' ELSE 'reject' END,
       decision_note, COALESCE(decided_at,updated_at)
FROM approvals
WHERE status IN ('approved','rejected') AND btrim(decided_by) <> ''
ON CONFLICT (tenant_id,approval_id,actor_id) DO NOTHING;

CREATE INDEX IF NOT EXISTS approval_decisions_approval_idx
    ON approval_decisions (tenant_id,approval_id,decided_at,id);

CREATE OR REPLACE FUNCTION reject_approval_decision_mutation()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'approval decisions are append-only';
END;
$$;

DROP TRIGGER IF EXISTS approval_decisions_no_update ON approval_decisions;
CREATE TRIGGER approval_decisions_no_update
BEFORE UPDATE ON approval_decisions FOR EACH ROW EXECUTE FUNCTION reject_approval_decision_mutation();

DROP TRIGGER IF EXISTS approval_decisions_no_delete ON approval_decisions;
CREATE TRIGGER approval_decisions_no_delete
BEFORE DELETE ON approval_decisions FOR EACH ROW EXECUTE FUNCTION reject_approval_decision_mutation();
