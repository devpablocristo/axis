-- platform:migrate:non-transactional
SET lock_timeout = '5s';
SET statement_timeout = '30s';

CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_axis_users_email_lower_unique
    ON axis_users (lower(email))
    WHERE email <> '';
