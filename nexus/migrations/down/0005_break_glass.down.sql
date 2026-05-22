-- Reversa de 0005_break_glass.up.sql
ALTER TABLE approvals DROP COLUMN IF EXISTS decisions;
ALTER TABLE approvals DROP COLUMN IF EXISTS required_approvals;
ALTER TABLE approvals DROP COLUMN IF EXISTS break_glass;
