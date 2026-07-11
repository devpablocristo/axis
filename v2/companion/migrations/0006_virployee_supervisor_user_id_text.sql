SET lock_timeout = '5s';
SET statement_timeout = '30s';

ALTER TABLE virployees
    ALTER COLUMN supervisor_user_id TYPE text USING supervisor_user_id::text;
