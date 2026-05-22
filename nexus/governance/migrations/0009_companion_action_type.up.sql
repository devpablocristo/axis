-- Action type for Nexus Companion: propose governed action from a task
INSERT INTO action_types (name, description, category, risk_class, reversible, requires_break_glass, enabled)
VALUES (
    'companion.propose',
    'Proponer una acción gobernada desde una tarea de Companion hacia Review',
    'companion',
    'low',
    true,
    false,
    true
)
ON CONFLICT (name) DO NOTHING;
