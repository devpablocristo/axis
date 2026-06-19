CREATE TABLE IF NOT EXISTS agent_profiles (
	id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
	profile_id TEXT NOT NULL CHECK (btrim(profile_id) <> ''),
	family_id TEXT NOT NULL CHECK (btrim(family_id) <> ''),
	version_label TEXT NOT NULL DEFAULT '',
	name TEXT NOT NULL CHECK (btrim(name) <> ''),
	description TEXT NOT NULL DEFAULT '',
	system_prompt TEXT NOT NULL CHECK (btrim(system_prompt) <> ''),
	max_autonomy TEXT NOT NULL DEFAULT 'A1'
		CHECK (max_autonomy IN ('A0', 'A1', 'A2', 'A3', 'A4', 'A5')),
	allowed_tools TEXT[] NOT NULL DEFAULT '{}',
	allowed_capabilities TEXT[] NOT NULL DEFAULT '{}',
	memory_policy_json JSONB NOT NULL DEFAULT '{}'::jsonb,
	llm_config_json JSONB NOT NULL DEFAULT '{}'::jsonb,
	enabled BOOLEAN NOT NULL DEFAULT true,
	archived_at TIMESTAMPTZ,
	created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
	updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
	UNIQUE (profile_id)
);

CREATE INDEX IF NOT EXISTS idx_agent_profiles_family
	ON agent_profiles (family_id, version_label);

CREATE INDEX IF NOT EXISTS idx_agent_profiles_active
	ON agent_profiles (profile_id)
	WHERE enabled = true AND archived_at IS NULL;

CREATE TABLE IF NOT EXISTS agent_profile_versions (
	id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
	agent_profile_id UUID NOT NULL REFERENCES agent_profiles(id) ON DELETE CASCADE,
	profile_id TEXT NOT NULL CHECK (btrim(profile_id) <> ''),
	family_id TEXT NOT NULL CHECK (btrim(family_id) <> ''),
	version_label TEXT NOT NULL DEFAULT '',
	name TEXT NOT NULL CHECK (btrim(name) <> ''),
	description TEXT NOT NULL DEFAULT '',
	system_prompt TEXT NOT NULL CHECK (btrim(system_prompt) <> ''),
	max_autonomy TEXT NOT NULL DEFAULT 'A1',
	allowed_tools TEXT[] NOT NULL DEFAULT '{}',
	allowed_capabilities TEXT[] NOT NULL DEFAULT '{}',
	memory_policy_json JSONB NOT NULL DEFAULT '{}'::jsonb,
	llm_config_json JSONB NOT NULL DEFAULT '{}'::jsonb,
	enabled BOOLEAN NOT NULL DEFAULT true,
	archived_at TIMESTAMPTZ,
	original_created_at TIMESTAMPTZ NOT NULL,
	original_updated_at TIMESTAMPTZ NOT NULL,
	saved_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_agent_profile_versions_profile
	ON agent_profile_versions (profile_id, saved_at DESC);

CREATE INDEX IF NOT EXISTS idx_agent_profile_versions_profile_id
	ON agent_profile_versions (agent_profile_id, saved_at DESC);

CREATE OR REPLACE FUNCTION save_agent_profile_previous_version()
RETURNS TRIGGER AS $$
BEGIN
	IF OLD.profile_id IS DISTINCT FROM NEW.profile_id
		OR OLD.family_id IS DISTINCT FROM NEW.family_id
		OR OLD.version_label IS DISTINCT FROM NEW.version_label
		OR OLD.name IS DISTINCT FROM NEW.name
		OR OLD.description IS DISTINCT FROM NEW.description
		OR OLD.system_prompt IS DISTINCT FROM NEW.system_prompt
		OR OLD.max_autonomy IS DISTINCT FROM NEW.max_autonomy
		OR OLD.allowed_tools IS DISTINCT FROM NEW.allowed_tools
		OR OLD.allowed_capabilities IS DISTINCT FROM NEW.allowed_capabilities
		OR OLD.memory_policy_json IS DISTINCT FROM NEW.memory_policy_json
		OR OLD.llm_config_json IS DISTINCT FROM NEW.llm_config_json
		OR OLD.enabled IS DISTINCT FROM NEW.enabled
		OR OLD.archived_at IS DISTINCT FROM NEW.archived_at THEN
		INSERT INTO agent_profile_versions (
			agent_profile_id,
			profile_id,
			family_id,
			version_label,
			name,
			description,
			system_prompt,
			max_autonomy,
			allowed_tools,
			allowed_capabilities,
			memory_policy_json,
			llm_config_json,
			enabled,
			archived_at,
			original_created_at,
			original_updated_at
		)
		VALUES (
			OLD.id,
			OLD.profile_id,
			OLD.family_id,
			OLD.version_label,
			OLD.name,
			OLD.description,
			OLD.system_prompt,
			OLD.max_autonomy,
			OLD.allowed_tools,
			OLD.allowed_capabilities,
			OLD.memory_policy_json,
			OLD.llm_config_json,
			OLD.enabled,
			OLD.archived_at,
			OLD.created_at,
			OLD.updated_at
		);
	END IF;
	RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_agent_profile_previous_version ON agent_profiles;

CREATE TRIGGER trg_agent_profile_previous_version
BEFORE UPDATE ON agent_profiles
FOR EACH ROW
EXECUTE FUNCTION save_agent_profile_previous_version();

INSERT INTO agent_profiles (
	profile_id,
	family_id,
	version_label,
	name,
	description,
	system_prompt,
	max_autonomy,
	allowed_tools,
	allowed_capabilities,
	memory_policy_json,
	llm_config_json,
	enabled
)
VALUES (
	'axis.ops.billing.v1',
	'axis.ops.billing',
	'v1',
	'Billing Agent',
	'Generic billing operations agent for connected product installations.',
	'You are Billing Agent, an Axis Ops agent.

Your job is to help with billing, plans, quotas, subscriptions, invoices, payment issues, and commercial adjustments for any connected product.

Rules:
- Use only the billing capabilities allowed for the current product installation.
- Read and explain billing state, plan limits, quotas, subscription status, and pending plan requests.
- Detect quota blocks or payment-related blockers from available billing tools.
- Normal plan changes are deterministic: a product plan may activate only after confirmed payment/subscription entitlement from the product billing system.
- For unpaid upgrade requests, return or explain payment_required; do not recommend approval as a substitute for payment.
- You may propose commercial adjustments, but you must not execute plan changes, discounts, credits, subscription changes, charges, refunds, or Stripe operations directly.
- Manual intervention is exceptional: suggest it only as a proposal/task with evidence, never execute it yourself.
- Any side effect requires a governed proposal through Nexus.
- Do not access product data unrelated to billing.
- Do not access clinical, private, medical, document, timeline, DICOM, or summary content.
- If billing evidence is insufficient, say what evidence is missing and stop.',
	'A1',
	ARRAY[]::TEXT[],
	ARRAY[]::TEXT[],
	'{"allowed_types":["operational","playbook"],"max_items":10}'::jsonb,
	'{"temperature":0,"max_tokens":1024}'::jsonb,
	true
)
ON CONFLICT (profile_id) DO NOTHING;
