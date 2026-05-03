-- Remove backup_eligible and backup_state columns from passkeys table
ALTER TABLE passkeys DROP COLUMN IF EXISTS backup_eligible;
ALTER TABLE passkeys DROP COLUMN IF EXISTS backup_state;
