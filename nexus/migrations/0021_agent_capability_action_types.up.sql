-- Generic agent capability action types.
-- Nexus remains agent-agnostic: requesters decide who they are; Nexus only
-- validates action type, risk and policy.

WITH seed(name, description, category, risk_class, schema, reversible, requires_break_glass, enabled) AS (
    VALUES
        (
            'agent.capability.invoke',
            'Invoke a governed external capability from an agent runtime',
            'agent_capability',
            'medium',
            '{
                "type": "object",
                "required": ["org_id", "operation", "payload", "action_binding"],
                "properties": {
                    "org_id": {"type": "string"},
                    "operation": {"type": "string"},
                    "payload": {"type": "object"},
                    "action_binding": {"type": "object"}
                }
            }'::jsonb,
            true,
            false,
            true
        ),
        (
            'agent.capability.compensate',
            'Request governed compensation or rollback for a previous capability invocation',
            'agent_capability',
            'high',
            '{
                "type": "object",
                "required": ["org_id", "task_id", "plan_step_id", "reason", "compensation", "action_binding"],
                "properties": {
                    "org_id": {"type": "string"},
                    "task_id": {"type": "string"},
                    "plan_step_id": {"type": "string"},
                    "reason": {"type": "string"},
                    "compensation": {"type": "object"},
                    "action_binding": {"type": "object"}
                }
            }'::jsonb,
            true,
            false,
            true
        )
)
INSERT INTO action_types (name, description, category, risk_class, schema, reversible, requires_break_glass, enabled)
SELECT name, description, category, risk_class, schema, reversible, requires_break_glass, enabled
FROM seed
WHERE NOT EXISTS (
    SELECT 1
    FROM action_types
    WHERE action_types.name = seed.name
      AND action_types.org_id IS NULL
);
