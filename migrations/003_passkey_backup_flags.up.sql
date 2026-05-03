-- Add backup_eligible and backup_state columns to passkeys table
-- These flags are required by the WebAuthn spec to track credential backup status
-- and must be stored to avoid "BackupEligible flag inconsistency" errors during login validation.

ALTER TABLE passkeys ADD COLUMN IF NOT EXISTS backup_eligible BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE passkeys ADD COLUMN IF NOT EXISTS backup_state BOOLEAN NOT NULL DEFAULT false;
