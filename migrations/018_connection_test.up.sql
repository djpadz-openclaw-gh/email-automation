-- Add connection test tracking columns to accounts
ALTER TABLE accounts ADD COLUMN IF NOT EXISTS last_connection_test_at TIMESTAMPTZ;
ALTER TABLE accounts ADD COLUMN IF NOT EXISTS last_connection_status TEXT;
ALTER TABLE accounts ADD COLUMN IF NOT EXISTS last_connection_error TEXT;
