ALTER TABLE accounts
    DROP COLUMN IF EXISTS oauth_refresh_token,
    DROP COLUMN IF EXISTS oauth_token_expiry,
    DROP COLUMN IF EXISTS oauth_provider;
