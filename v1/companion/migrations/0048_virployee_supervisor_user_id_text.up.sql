ALTER TABLE companion_virployees
    ALTER COLUMN supervisor_user_id TYPE TEXT
    USING supervisor_user_id::TEXT;

ALTER TABLE companion_virployees
    ADD CONSTRAINT companion_virployees_supervisor_user_required
    CHECK (supervisor_user_id <> '');
