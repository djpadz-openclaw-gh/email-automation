-- Remove connection test tracking columns from accounts
ALTER TABLE accounts DROP COLUMN IF EXISTS last_connection_test_at;
ALTER TABLE accounts DROP COLUMN IF EXISTS last_connection_status;
ALTER TABLE accounts DROP COLUMN IF EXISTS last_connection_error;
