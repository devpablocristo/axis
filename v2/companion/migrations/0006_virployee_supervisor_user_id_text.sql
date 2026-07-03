ALTER TABLE virployees
    ALTER COLUMN supervisor_user_id TYPE text USING supervisor_user_id::text;
