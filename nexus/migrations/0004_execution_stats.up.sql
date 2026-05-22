-- Estadísticas de ejecución por action_type para el feedback loop
-- El cascade risk scoring usa success_rate para ajustar el riesgo dinámicamente
CREATE TABLE IF NOT EXISTS execution_stats (
    action_type TEXT PRIMARY KEY,
    success_count INTEGER NOT NULL DEFAULT 0,
    failure_count INTEGER NOT NULL DEFAULT 0,
    last_success_at TIMESTAMPTZ,
    last_failure_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
