ALTER TABLE virployees
    ADD COLUMN IF NOT EXISTS autonomy text NOT NULL DEFAULT 'A1';

UPDATE virployees
SET autonomy = 'A1'
WHERE autonomy IS NULL
   OR btrim(autonomy) = ''
   OR autonomy NOT IN ('A0', 'A1', 'A2', 'A3', 'A4', 'A5');

ALTER TABLE virployees
    ALTER COLUMN autonomy SET DEFAULT 'A1',
    ALTER COLUMN autonomy SET NOT NULL;

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
