-- OAuth2 support for Microsoft 365 and Gmail accounts
-- Adds columns for storing OAuth2 refresh tokens and metadata

ALTER TABLE accounts
    ADD COLUMN IF NOT EXISTS oauth_refresh_token TEXT DEFAULT '',
    ADD COLUMN IF NOT EXISTS oauth_token_expiry TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS oauth_provider TEXT DEFAULT '';

-- oauth_provider values: 'microsoft365', 'gmail', '' (for plain IMAP)
-- oauth_token stores the current access token (encrypted)
-- oauth_refresh_token stores the refresh token (encrypted)
-- oauth_token_expiry tracks when the access token expires

COMMENT ON COLUMN accounts.oauth_provider IS 'OAuth2 provider: microsoft365, gmail, or empty for plain IMAP';
COMMENT ON COLUMN accounts.oauth_refresh_token IS 'Encrypted OAuth2 refresh token';
COMMENT ON COLUMN accounts.oauth_token_expiry IS 'When the current access token expires';
