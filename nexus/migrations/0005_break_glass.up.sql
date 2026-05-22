-- Break-glass: múltiples aprobadores para operaciones críticas
ALTER TABLE approvals ADD COLUMN IF NOT EXISTS break_glass BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE approvals ADD COLUMN IF NOT EXISTS required_approvals INTEGER NOT NULL DEFAULT 1;
ALTER TABLE approvals ADD COLUMN IF NOT EXISTS decisions JSONB NOT NULL DEFAULT '[]';
