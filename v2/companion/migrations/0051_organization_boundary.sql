SET lock_timeout = '5s';
SET statement_timeout = '30s';

-- Organization is the only ownership/isolation boundary in v2.
DO $$
DECLARE
    target record;
BEGIN
    FOR target IN
        SELECT c.table_schema, c.table_name
        FROM information_schema.columns c
        WHERE c.table_schema = current_schema()
          AND c.column_name = 'tenant_id'
          AND NOT EXISTS (
              SELECT 1
              FROM information_schema.columns existing
              WHERE existing.table_schema = c.table_schema
                AND existing.table_name = c.table_name
                AND existing.column_name = 'org_id'
          )
    LOOP
        EXECUTE format('ALTER TABLE %I.%I RENAME COLUMN tenant_id TO org_id', target.table_schema, target.table_name);
    END LOOP;
END $$;
