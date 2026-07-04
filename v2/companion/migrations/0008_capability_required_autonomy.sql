ALTER TABLE capabilities
    ADD COLUMN IF NOT EXISTS required_autonomy text;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_name = 'capabilities'
          AND column_name = 'action_class'
    ) THEN
        EXECUTE $sql$
            UPDATE capabilities
            SET required_autonomy = CASE action_class
                WHEN 'observe' THEN 'A0'
                WHEN 'recommend' THEN 'A1'
                WHEN 'draft' THEN 'A2'
                WHEN 'write_low' THEN 'A3'
                WHEN 'write_medium' THEN 'A4'
                WHEN 'write_high' THEN 'A5'
                ELSE required_autonomy
            END
            WHERE required_autonomy IS NULL
        $sql$;
    END IF;
END $$;

ALTER TABLE capabilities
    ALTER COLUMN required_autonomy SET NOT NULL;

ALTER TABLE capabilities
    DROP CONSTRAINT IF EXISTS capabilities_action_class_check;

ALTER TABLE capabilities
    DROP CONSTRAINT IF EXISTS capabilities_required_autonomy_check;

ALTER TABLE capabilities
    ADD CONSTRAINT capabilities_required_autonomy_check
    CHECK (required_autonomy IN ('A0', 'A1', 'A2', 'A3', 'A4', 'A5'));

ALTER TABLE capabilities
    DROP COLUMN IF EXISTS action_class;
