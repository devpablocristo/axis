SET lock_timeout = '5s';
SET statement_timeout = '30s';

-- V1 used companion_memories for a different product-level memory model in
-- shared databases. Preserve that table and give v2 Virployee memory an
-- unambiguous name. Existing v2 databases are renamed without data loss;
-- databases that still contain the v1 shape are left untouched.
DO $$
BEGIN
    IF to_regclass('companion_virployee_memories') IS NULL
       AND EXISTS (
           SELECT 1
           FROM information_schema.columns
           WHERE table_schema = current_schema()
             AND table_name = 'companion_memories'
             AND column_name = 'virployee_id'
       ) THEN
        ALTER TABLE companion_memories RENAME TO companion_virployee_memories;
    END IF;

    IF to_regclass('companion_virployee_memory_audit') IS NULL
       AND EXISTS (
           SELECT 1
           FROM information_schema.columns
           WHERE table_schema = current_schema()
             AND table_name = 'companion_memory_audit'
             AND column_name = 'virployee_id'
       ) THEN
        ALTER TABLE companion_memory_audit RENAME TO companion_virployee_memory_audit;
    END IF;
END $$;
