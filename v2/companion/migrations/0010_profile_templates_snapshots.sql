DO $$
BEGIN
    IF to_regclass('public.profile_templates') IS NULL
        AND to_regclass('public.virployee_profiles') IS NOT NULL
        AND NOT EXISTS (
            SELECT 1
            FROM information_schema.columns
            WHERE table_schema = 'public'
              AND table_name = 'virployee_profiles'
              AND column_name = 'virployee_id'
        )
    THEN
        ALTER TABLE virployee_profiles RENAME TO profile_templates;
        ALTER INDEX IF EXISTS idx_virployee_profiles_lifecycle RENAME TO idx_profile_templates_lifecycle;
        ALTER INDEX IF EXISTS idx_virployee_profiles_tenant_id RENAME TO idx_profile_templates_tenant_id;
        ALTER TABLE profile_templates
            RENAME CONSTRAINT virployee_profiles_max_autonomy_check TO profile_templates_max_autonomy_check;
    END IF;
END $$;

CREATE TABLE IF NOT EXISTS profile_templates (
    id uuid PRIMARY KEY,
    tenant_id text NOT NULL DEFAULT 'default',
    name text NOT NULL,
    description text NOT NULL DEFAULT '',
    system_prompt text NOT NULL,
    max_autonomy text NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    archived_at timestamptz NULL,
    trashed_at timestamptz NULL,
    purge_after timestamptz NULL,
    CONSTRAINT profile_templates_max_autonomy_check CHECK (
        max_autonomy IN ('A0', 'A1', 'A2', 'A3', 'A4', 'A5')
    )
);

CREATE INDEX IF NOT EXISTS idx_profile_templates_lifecycle
    ON profile_templates (tenant_id, archived_at, trashed_at);

CREATE INDEX IF NOT EXISTS idx_profile_templates_tenant_id
    ON profile_templates (tenant_id, id);

CREATE TABLE IF NOT EXISTS virployee_profiles (
    id uuid PRIMARY KEY,
    tenant_id text NOT NULL DEFAULT 'default',
    virployee_id uuid NOT NULL REFERENCES virployees(id) ON DELETE CASCADE,
    profile_template_id uuid NOT NULL,
    name text NOT NULL,
    description text NOT NULL DEFAULT '',
    system_prompt text NOT NULL,
    max_autonomy text NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CONSTRAINT virployee_profiles_virployee_unique UNIQUE (tenant_id, virployee_id),
    CONSTRAINT virployee_profiles_max_autonomy_check CHECK (
        max_autonomy IN ('A0', 'A1', 'A2', 'A3', 'A4', 'A5')
    )
);

CREATE INDEX IF NOT EXISTS idx_virployee_profiles_template_id
    ON virployee_profiles (tenant_id, profile_template_id);

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_schema = 'public'
          AND table_name = 'virployees'
          AND column_name = 'profile_id'
    ) THEN
        INSERT INTO virployee_profiles (
            id,
            tenant_id,
            virployee_id,
            profile_template_id,
            name,
            description,
            system_prompt,
            max_autonomy,
            created_at,
            updated_at
        )
        SELECT
            gen_random_uuid(),
            v.tenant_id,
            v.id,
            pt.id,
            pt.name,
            pt.description,
            pt.system_prompt,
            pt.max_autonomy,
            now(),
            now()
        FROM virployees v
        JOIN profile_templates pt
            ON pt.id = v.profile_id
            AND pt.tenant_id = v.tenant_id
        WHERE v.profile_id IS NOT NULL
          AND NOT EXISTS (
            SELECT 1
            FROM virployee_profiles vp
            WHERE vp.tenant_id = v.tenant_id
              AND vp.virployee_id = v.id
          );

        ALTER TABLE virployees DROP CONSTRAINT IF EXISTS virployees_profile_id_fkey;
        DROP INDEX IF EXISTS idx_virployees_profile_id;
        ALTER TABLE virployees DROP COLUMN IF EXISTS profile_id;
    END IF;
END $$;
