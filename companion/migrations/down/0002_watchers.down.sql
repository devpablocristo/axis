-- Reversa de 0002_watchers.up.sql
DROP INDEX IF EXISTS idx_proposals_review;
DROP INDEX IF EXISTS idx_proposals_org_status;
DROP INDEX IF EXISTS idx_proposals_watcher;
DROP TABLE IF EXISTS companion_proposals;

DROP INDEX IF EXISTS idx_watchers_type;
DROP INDEX IF EXISTS idx_watchers_org_enabled;
DROP TABLE IF EXISTS companion_watchers;
