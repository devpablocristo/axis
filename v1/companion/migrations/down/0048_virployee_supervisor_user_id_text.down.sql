ALTER TABLE companion_virployees
    DROP CONSTRAINT IF EXISTS companion_virployees_supervisor_user_required;

ALTER TABLE companion_virployees
    ALTER COLUMN supervisor_user_id TYPE UUID
    USING CASE
        WHEN supervisor_user_id ~* '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
            THEN supervisor_user_id::UUID
        ELSE '00000000-0000-0000-0000-000000000000'::UUID
    END;
