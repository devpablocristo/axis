SET lock_timeout = '5s';
SET statement_timeout = '30s';

ALTER TABLE virployees
    ADD COLUMN IF NOT EXISTS autonomy text NOT NULL DEFAULT 'A1';

UPDATE virployees
SET autonomy = 'A1'
WHERE autonomy IS NULL
   OR btrim(autonomy) = ''
   OR autonomy NOT IN ('A0', 'A1', 'A2', 'A3', 'A4', 'A5');

ALTER TABLE virployees
    DROP CONSTRAINT IF EXISTS virployees_autonomy_not_null;

ALTER TABLE virployees
    ADD CONSTRAINT virployees_autonomy_not_null
    CHECK (autonomy IS NOT NULL) NOT VALID;

ALTER TABLE virployees
    VALIDATE CONSTRAINT virployees_autonomy_not_null;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'virployees_autonomy_check'
          AND conrelid = 'virployees'::regclass
    ) THEN
        ALTER TABLE virployees
            ADD CONSTRAINT virployees_autonomy_check
            CHECK (autonomy IN ('A0', 'A1', 'A2', 'A3', 'A4', 'A5'));
    END IF;
END $$;
