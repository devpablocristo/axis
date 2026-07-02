CREATE TABLE IF NOT EXISTS assist_packs (
	id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
	org_id TEXT NOT NULL CHECK (btrim(org_id) <> ''),
	owner_system TEXT NOT NULL CHECK (btrim(owner_system) <> ''),
	product_surface TEXT NOT NULL CHECK (btrim(product_surface) <> ''),
	assist_type TEXT NOT NULL CHECK (btrim(assist_type) <> ''),
	name TEXT NOT NULL CHECK (btrim(name) <> ''),
	description TEXT NOT NULL DEFAULT '',
	input_contract TEXT NOT NULL CHECK (btrim(input_contract) <> ''),
	output_contract TEXT NOT NULL CHECK (btrim(output_contract) <> ''),
	prompt_template TEXT NOT NULL CHECK (btrim(prompt_template) <> ''),
	model_policy_json JSONB NOT NULL DEFAULT '{}'::jsonb,
	enabled BOOLEAN NOT NULL DEFAULT true,
	archived_at TIMESTAMPTZ,
	created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
	updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
	UNIQUE (org_id, owner_system, product_surface, assist_type)
);

CREATE INDEX IF NOT EXISTS idx_assist_packs_scope
	ON assist_packs (org_id, owner_system, product_surface, assist_type)
	WHERE archived_at IS NULL;

CREATE TABLE IF NOT EXISTS assist_runs (
	id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
	org_id TEXT NOT NULL CHECK (btrim(org_id) <> ''),
	pack_id UUID NOT NULL REFERENCES assist_packs(id) ON DELETE RESTRICT,
	owner_system TEXT NOT NULL CHECK (btrim(owner_system) <> ''),
	product_surface TEXT NOT NULL CHECK (btrim(product_surface) <> ''),
	assist_type TEXT NOT NULL CHECK (btrim(assist_type) <> ''),
	subject_type TEXT NOT NULL DEFAULT '',
	subject_id TEXT NOT NULL DEFAULT '',
	input_json JSONB NOT NULL DEFAULT '{}'::jsonb,
	output_json JSONB NOT NULL DEFAULT '{}'::jsonb,
	status TEXT NOT NULL CHECK (status IN ('running', 'completed', 'failed')),
	error_message TEXT NOT NULL DEFAULT '',
	created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
	completed_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_assist_runs_scope
	ON assist_runs (org_id, owner_system, product_surface, assist_type);

CREATE INDEX IF NOT EXISTS idx_assist_runs_subject
	ON assist_runs (org_id, product_surface, subject_id);
