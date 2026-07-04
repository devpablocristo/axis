ALTER TABLE virployees
    ADD COLUMN IF NOT EXISTS profile_template_id uuid NULL;

DO $$
BEGIN
    IF to_regclass('public.virployee_profiles') IS NOT NULL THEN
        UPDATE virployees v
        SET profile_template_id = vp.profile_template_id
        FROM virployee_profiles vp
        WHERE v.tenant_id = vp.tenant_id
          AND v.id = vp.virployee_id
          AND v.profile_template_id IS NULL;
    END IF;
END $$;

DELETE FROM virployees
WHERE profile_template_id IS NULL;

ALTER TABLE virployees
    ALTER COLUMN profile_template_id SET NOT NULL;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'virployees_profile_template_id_fkey'
    ) THEN
        ALTER TABLE virployees
            ADD CONSTRAINT virployees_profile_template_id_fkey
            FOREIGN KEY (profile_template_id) REFERENCES profile_templates(id);
    END IF;
END $$;

CREATE INDEX IF NOT EXISTS idx_virployees_profile_template_id
    ON virployees (tenant_id, profile_template_id);

DROP TABLE IF EXISTS virployee_profiles;
