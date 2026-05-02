-- Rollback authentication tables

DROP TABLE IF EXISTS login_attempts;
DROP TABLE IF EXISTS token_blacklist;
DROP TABLE IF EXISTS api_keys;
DROP TABLE IF EXISTS passkeys;

-- Remove user_id from tenants
ALTER TABLE tenants DROP COLUMN IF EXISTS user_id;

DROP TABLE IF EXISTS users;
