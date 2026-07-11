SET lock_timeout = '5s';
SET statement_timeout = '30s';

CREATE TABLE IF NOT EXISTS job_roles (
    id uuid PRIMARY KEY,
    tenant_id text NOT NULL DEFAULT 'default',
    name text NOT NULL,
    slug text NOT NULL,
    mission text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    archived_at timestamptz NULL,
    trashed_at timestamptz NULL,
    purge_after timestamptz NULL,
    CONSTRAINT job_roles_tenant_slug_unique UNIQUE (tenant_id, slug)
);

ALTER TABLE virployees
    ADD COLUMN IF NOT EXISTS job_role_id uuid;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_name = 'virployees'
          AND column_name = 'role'
    ) THEN
        EXECUTE $sql$
            WITH distinct_roles AS (
                SELECT DISTINCT
                    tenant_id,
                    COALESCE(NULLIF(btrim(role), ''), 'Job Role') AS role_name
                FROM virployees
                WHERE job_role_id IS NULL
            ),
            slugged AS (
                SELECT
                    tenant_id,
                    role_name,
                    COALESCE(
                        NULLIF(trim(both '-' from regexp_replace(lower(role_name), '[^a-z0-9]+', '-', 'g')), ''),
                        'job-role'
                    ) AS base_slug
                FROM distinct_roles
            ),
            numbered AS (
                SELECT
                    tenant_id,
                    role_name,
                    CASE
                        WHEN row_number() OVER (PARTITION BY tenant_id, base_slug ORDER BY role_name) = 1
                            THEN base_slug
                        ELSE base_slug || '-' || row_number() OVER (PARTITION BY tenant_id, base_slug ORDER BY role_name)::text
                    END AS slug
                FROM slugged
            )
            INSERT INTO job_roles (
                id, tenant_id, name, slug, mission, created_at, updated_at
            )
            SELECT
                gen_random_uuid(),
                tenant_id,
                role_name,
                slug,
                '',
                now(),
                now()
            FROM numbered
            ON CONFLICT (tenant_id, slug) DO NOTHING
        $sql$;

        EXECUTE $sql$
            WITH distinct_roles AS (
                SELECT DISTINCT
                    tenant_id,
                    COALESCE(NULLIF(btrim(role), ''), 'Job Role') AS role_name
                FROM virployees
                WHERE job_role_id IS NULL
            ),
            slugged AS (
                SELECT
                    tenant_id,
                    role_name,
                    COALESCE(
                        NULLIF(trim(both '-' from regexp_replace(lower(role_name), '[^a-z0-9]+', '-', 'g')), ''),
                        'job-role'
                    ) AS base_slug
                FROM distinct_roles
            ),
            numbered AS (
                SELECT
                    tenant_id,
                    role_name,
                    CASE
                        WHEN row_number() OVER (PARTITION BY tenant_id, base_slug ORDER BY role_name) = 1
                            THEN base_slug
                        ELSE base_slug || '-' || row_number() OVER (PARTITION BY tenant_id, base_slug ORDER BY role_name)::text
                    END AS slug
                FROM slugged
            )
            UPDATE virployees v
            SET job_role_id = jr.id
            FROM numbered n
            JOIN job_roles jr
              ON jr.tenant_id = n.tenant_id
             AND jr.slug = n.slug
            WHERE v.job_role_id IS NULL
              AND v.tenant_id = n.tenant_id
              AND COALESCE(NULLIF(btrim(v.role), ''), 'Job Role') = n.role_name
        $sql$;
    END IF;
END $$;

ALTER TABLE virployees
    DROP CONSTRAINT IF EXISTS virployees_job_role_id_not_null;

ALTER TABLE virployees
    ADD CONSTRAINT virployees_job_role_id_not_null
    CHECK (job_role_id IS NOT NULL) NOT VALID;

ALTER TABLE virployees
    VALIDATE CONSTRAINT virployees_job_role_id_not_null;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'virployees_job_role_id_fkey'
          AND conrelid = 'virployees'::regclass
    ) THEN
        ALTER TABLE virployees
            ADD CONSTRAINT virployees_job_role_id_fkey
            FOREIGN KEY (job_role_id)
            REFERENCES job_roles(id);
    END IF;
END $$;

ALTER TABLE virployees
    DROP COLUMN IF EXISTS role;
