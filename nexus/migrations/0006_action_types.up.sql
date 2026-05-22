-- Ontología tipada de acciones
-- Cada action_type tiene schema de validación, clase de riesgo y metadata
CREATE TABLE IF NOT EXISTS action_types (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name                 TEXT NOT NULL UNIQUE,
    description          TEXT NOT NULL DEFAULT '',
    category             TEXT NOT NULL DEFAULT '',
    risk_class           TEXT NOT NULL DEFAULT 'low',
    schema               JSONB NOT NULL DEFAULT '{}',
    reversible           BOOLEAN NOT NULL DEFAULT true,
    requires_break_glass BOOLEAN NOT NULL DEFAULT false,
    enabled              BOOLEAN NOT NULL DEFAULT true,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Seed: action types por defecto
INSERT INTO action_types (name, description, category, risk_class, reversible, requires_break_glass) VALUES
    ('alert.silence', 'Silenciar una alerta por un período', 'alert', 'high', true, false),
    ('alert.escalate', 'Escalar una alerta a un equipo', 'alert', 'low', true, false),
    ('runbook.execute', 'Ejecutar un runbook automatizado', 'runbook', 'high', false, true),
    ('incident.resolve', 'Resolver un incidente abierto', 'incident', 'medium', true, false),
    ('config.update', 'Actualizar configuración de un servicio', 'config', 'medium', true, false),
    ('deploy.trigger', 'Disparar un deploy a un ambiente', 'deploy', 'medium', false, false),
    ('delete', 'Eliminar un recurso permanentemente', 'data', 'critical', false, true),
    ('iam.grant_role', 'Otorgar un rol/permiso a un actor', 'iam', 'high', true, false),
    ('treasury.transfer', 'Transferir fondos entre cuentas', 'treasury', 'critical', false, true)
ON CONFLICT (name) DO NOTHING;
