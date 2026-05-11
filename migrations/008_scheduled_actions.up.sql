-- Add status and executed_at columns to deferred_actions for better observability.
-- The schedule() Lua function uses this table to store scheduled actions.
-- Status: 'pending', 'executed', 'failed'
ALTER TABLE deferred_actions ADD COLUMN IF NOT EXISTS status TEXT NOT NULL DEFAULT 'pending';
ALTER TABLE deferred_actions ADD COLUMN IF NOT EXISTS executed_at TIMESTAMPTZ;

-- Backfill existing rows
UPDATE deferred_actions SET status = 'executed', executed_at = created_at WHERE executed = true AND error = '';
UPDATE deferred_actions SET status = 'failed', executed_at = created_at WHERE executed = true AND error != '';

-- Index for the scheduler to efficiently find pending actions
CREATE INDEX IF NOT EXISTS idx_deferred_actions_pending
    ON deferred_actions (execute_at)
    WHERE status = 'pending';
