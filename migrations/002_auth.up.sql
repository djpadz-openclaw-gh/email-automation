-- Authentication tables: users, passkeys, api_keys
-- Adds user-based authentication with password, TOTP, and WebAuthn support

CREATE TABLE IF NOT EXISTS users (
    id            BIGSERIAL PRIMARY KEY,
    username      TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    totp_secret   TEXT,
    totp_enabled  BOOLEAN NOT NULL DEFAULT false,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_users_username ON users(username);

CREATE TABLE IF NOT EXISTS passkeys (
    id            BIGSERIAL PRIMARY KEY,
    user_id       BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    credential_id TEXT NOT NULL UNIQUE,
    public_key    TEXT NOT NULL,
    sign_count    BIGINT NOT NULL DEFAULT 0,
    transports    JSONB DEFAULT '[]',
    name          TEXT NOT NULL DEFAULT 'Passkey',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_used_at  TIMESTAMPTZ
);

CREATE INDEX idx_passkeys_user ON passkeys(user_id);
CREATE INDEX idx_passkeys_credential ON passkeys(credential_id);

CREATE TABLE IF NOT EXISTS api_keys (
    id           BIGSERIAL PRIMARY KEY,
    user_id      BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    key_hash     TEXT NOT NULL UNIQUE,
    name         TEXT NOT NULL,
    prefix       TEXT NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_used_at TIMESTAMPTZ
);

CREATE INDEX idx_api_keys_user ON api_keys(user_id);
CREATE INDEX idx_api_keys_hash ON api_keys(key_hash);

-- Add user_id to tenants (each tenant belongs to a user)
ALTER TABLE tenants ADD COLUMN IF NOT EXISTS user_id BIGINT REFERENCES users(id) ON DELETE SET NULL;
CREATE INDEX IF NOT EXISTS idx_tenants_user ON tenants(user_id);

-- Token blacklist for logout
CREATE TABLE IF NOT EXISTS token_blacklist (
    token_hash  TEXT PRIMARY KEY,
    expires_at  TIMESTAMPTZ NOT NULL
);

CREATE INDEX idx_token_blacklist_expires ON token_blacklist(expires_at);

-- Rate limiting table
CREATE TABLE IF NOT EXISTS login_attempts (
    id         BIGSERIAL PRIMARY KEY,
    username   TEXT NOT NULL,
    ip_address TEXT NOT NULL,
    success    BOOLEAN NOT NULL DEFAULT false,
    attempted_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_login_attempts_user_time ON login_attempts(username, attempted_at);
CREATE INDEX idx_login_attempts_ip_time ON login_attempts(ip_address, attempted_at);
