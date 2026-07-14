-- Enrich the capability contract with governance metadata (Fase 1, PR1).
-- Fail-safe defaults: an unconfigured capability is treated as maximally
-- governed (high risk, has side effects, requires approval). Additive and
-- legacy-safe: existing rows adopt the defaults and behavior is unchanged
-- until Fase 2 consumes these fields.
SET lock_timeout = '5s';
SET statement_timeout = '30s';

ALTER TABLE capabilities
    ADD COLUMN IF NOT EXISTS risk_class text NOT NULL DEFAULT 'high',
    ADD COLUMN IF NOT EXISTS side_effect_class text NOT NULL DEFAULT 'write',
    ADD COLUMN IF NOT EXISTS requires_nexus_approval boolean NOT NULL DEFAULT true,
    ADD COLUMN IF NOT EXISTS evidence_required boolean NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS rollback_capability_key text NOT NULL DEFAULT '';

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'capabilities_risk_class_check') THEN
        ALTER TABLE capabilities
            ADD CONSTRAINT capabilities_risk_class_check CHECK (risk_class IN ('low', 'medium', 'high'));
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'capabilities_side_effect_class_check') THEN
        ALTER TABLE capabilities
            ADD CONSTRAINT capabilities_side_effect_class_check CHECK (side_effect_class IN ('read', 'write'));
    END IF;
END$$;
