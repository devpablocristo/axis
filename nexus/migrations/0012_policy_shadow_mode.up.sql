-- Agrega modo shadow a policies: evalúa sin actuar.
ALTER TABLE policies ADD COLUMN IF NOT EXISTS mode TEXT NOT NULL DEFAULT 'enforced';
ALTER TABLE policies ADD COLUMN IF NOT EXISTS shadow_hits INTEGER NOT NULL DEFAULT 0;
