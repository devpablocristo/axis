CREATE UNIQUE INDEX IF NOT EXISTS idx_axis_users_email_lower_unique
    ON axis_users (lower(email))
    WHERE email <> '';
