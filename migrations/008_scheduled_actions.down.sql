DROP INDEX IF EXISTS idx_deferred_actions_pending;
ALTER TABLE deferred_actions DROP COLUMN IF EXISTS status;
ALTER TABLE deferred_actions DROP COLUMN IF EXISTS executed_at;
