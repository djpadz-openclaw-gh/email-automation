-- Add last_uid_processed to accounts for UID-based polling
-- This replaces the unseen-flag approach to prevent reprocessing old messages.
ALTER TABLE accounts ADD COLUMN IF NOT EXISTS last_uid_processed BIGINT NOT NULL DEFAULT 0;
