CREATE TABLE IF NOT EXISTS finding_rules (
	id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
	org_id TEXT NOT NULL CHECK (btrim(org_id) <> ''),
	owner_system TEXT NOT NULL CHECK (btrim(owner_system) <> ''),
	source_system TEXT NOT NULL CHECK (btrim(source_system) <> ''),
	fact_type TEXT NOT NULL CHECK (btrim(fact_type) <> ''),
	code TEXT NOT NULL CHECK (btrim(code) <> ''),
	name TEXT NOT NULL CHECK (btrim(name) <> ''),
	description TEXT NOT NULL DEFAULT '',
	expression TEXT NOT NULL CHECK (btrim(expression) <> ''),
	severity TEXT NOT NULL CHECK (severity IN ('info', 'warning', 'critical')),
	title TEXT NOT NULL CHECK (btrim(title) <> ''),
	message TEXT NOT NULL CHECK (btrim(message) <> ''),
	recommendation TEXT NOT NULL DEFAULT '',
	mode TEXT NOT NULL DEFAULT 'enforced' CHECK (mode IN ('enforced', 'shadow')),
	enabled BOOLEAN NOT NULL DEFAULT true,
	priority INTEGER NOT NULL DEFAULT 100,
	archived_at TIMESTAMPTZ,
	created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
	updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
	UNIQUE (org_id, owner_system, code)
);

CREATE INDEX IF NOT EXISTS idx_finding_rules_scope
	ON finding_rules (org_id, owner_system, source_system, fact_type)
	WHERE archived_at IS NULL;

CREATE TABLE IF NOT EXISTS fact_evaluations (
	id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
	org_id TEXT NOT NULL CHECK (btrim(org_id) <> ''),
	owner_system TEXT NOT NULL CHECK (btrim(owner_system) <> ''),
	source_system TEXT NOT NULL CHECK (btrim(source_system) <> ''),
	fact_type TEXT NOT NULL CHECK (btrim(fact_type) <> ''),
	source_event_id TEXT NOT NULL CHECK (btrim(source_event_id) <> ''),
	subject_type TEXT NOT NULL DEFAULT '',
	subject_id TEXT NOT NULL DEFAULT '',
	facts_json JSONB NOT NULL DEFAULT '{}'::jsonb,
	created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
	UNIQUE (org_id, source_system, fact_type, source_event_id)
);

CREATE INDEX IF NOT EXISTS idx_fact_evaluations_subject
	ON fact_evaluations (org_id, source_system, subject_id);

CREATE TABLE IF NOT EXISTS findings (
	id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
	org_id TEXT NOT NULL CHECK (btrim(org_id) <> ''),
	evaluation_id UUID NOT NULL REFERENCES fact_evaluations(id) ON DELETE CASCADE,
	rule_id UUID REFERENCES finding_rules(id) ON DELETE SET NULL,
	owner_system TEXT NOT NULL CHECK (btrim(owner_system) <> ''),
	source_system TEXT NOT NULL CHECK (btrim(source_system) <> ''),
	fact_type TEXT NOT NULL CHECK (btrim(fact_type) <> ''),
	source_event_id TEXT NOT NULL CHECK (btrim(source_event_id) <> ''),
	subject_type TEXT NOT NULL DEFAULT '',
	subject_id TEXT NOT NULL DEFAULT '',
	code TEXT NOT NULL CHECK (btrim(code) <> ''),
	severity TEXT NOT NULL CHECK (severity IN ('info', 'warning', 'critical')),
	title TEXT NOT NULL CHECK (btrim(title) <> ''),
	message TEXT NOT NULL CHECK (btrim(message) <> ''),
	recommendation TEXT NOT NULL DEFAULT '',
	evidence_json JSONB NOT NULL DEFAULT '{}'::jsonb,
	status TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'acknowledged', 'resolved', 'dismissed', 'shadow')),
	resolution_note TEXT NOT NULL DEFAULT '',
	created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
	updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
	UNIQUE (evaluation_id, code)
);

CREATE INDEX IF NOT EXISTS idx_findings_scope
	ON findings (org_id, owner_system, source_system, fact_type);

CREATE INDEX IF NOT EXISTS idx_findings_subject
	ON findings (org_id, source_system, subject_id);
