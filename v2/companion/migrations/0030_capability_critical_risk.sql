SET lock_timeout = '5s';
SET statement_timeout = '30s';

ALTER TABLE capabilities DROP CONSTRAINT IF EXISTS capabilities_risk_class_check;
ALTER TABLE capabilities ADD CONSTRAINT capabilities_risk_class_check
    CHECK (risk_class IN ('low', 'medium', 'high', 'critical')) NOT VALID;
ALTER TABLE capabilities VALIDATE CONSTRAINT capabilities_risk_class_check;
