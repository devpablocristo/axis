SET lock_timeout = '5s';
SET statement_timeout = '30s';

ALTER TABLE axis_tenants DROP COLUMN IF EXISTS name;
