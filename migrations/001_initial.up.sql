-- Email Automation Schema
-- Multi-tenant, multi-account email rule engine

CREATE TABLE IF NOT EXISTS tenants (
    id          BIGSERIAL PRIMARY KEY,
    name        TEXT NOT NULL,
    slug        TEXT NOT NULL UNIQUE,
    api_key     TEXT NOT NULL UNIQUE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS accounts (
    id            BIGSERIAL PRIMARY KEY,
    tenant_id     BIGINT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    name          TEXT NOT NULL,
    email         TEXT NOT NULL,
    provider      TEXT NOT NULL DEFAULT 'imap',  -- imap, graph, mapi
    imap_host     TEXT,
    imap_port     INT DEFAULT 993,
    imap_tls      BOOLEAN DEFAULT true,
    username      TEXT,
    password      TEXT,                          -- encrypted at rest
    oauth_token   TEXT,
    active        BOOLEAN DEFAULT true,
    last_sync_at  TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_accounts_tenant ON accounts(tenant_id);
CREATE INDEX idx_accounts_active ON accounts(active) WHERE active = true;

CREATE TABLE IF NOT EXISTS rules (
    id          BIGSERIAL PRIMARY KEY,
    tenant_id   BIGINT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    description TEXT DEFAULT '',
    lua_code    TEXT NOT NULL,
    priority    INT NOT NULL DEFAULT 100,
    active      BOOLEAN DEFAULT true,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_rules_tenant ON rules(tenant_id);
CREATE INDEX idx_rules_priority ON rules(tenant_id, priority, id);

-- Many-to-many: which rules apply to which accounts (empty = all)
CREATE TABLE IF NOT EXISTS rule_accounts (
    rule_id    BIGINT NOT NULL REFERENCES rules(id) ON DELETE CASCADE,
    account_id BIGINT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    PRIMARY KEY (rule_id, account_id)
);

CREATE TABLE IF NOT EXISTS rule_execution_log (
    id          BIGSERIAL PRIMARY KEY,
    rule_id     BIGINT NOT NULL REFERENCES rules(id) ON DELETE CASCADE,
    account_id  BIGINT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    message_id  TEXT NOT NULL,
    subject     TEXT DEFAULT '',
    sender      TEXT DEFAULT '',
    action      TEXT NOT NULL,
    target      TEXT DEFAULT '',
    success     BOOLEAN NOT NULL DEFAULT true,
    error       TEXT DEFAULT '',
    executed_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_exec_log_rule ON rule_execution_log(rule_id);
CREATE INDEX idx_exec_log_account ON rule_execution_log(account_id);
CREATE INDEX idx_exec_log_time ON rule_execution_log(executed_at DESC);

CREATE TABLE IF NOT EXISTS deferred_actions (
    id          BIGSERIAL PRIMARY KEY,
    rule_id     BIGINT NOT NULL REFERENCES rules(id) ON DELETE CASCADE,
    account_id  BIGINT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    message_id  TEXT NOT NULL,
    action      TEXT NOT NULL,
    target      TEXT DEFAULT '',
    execute_at  TIMESTAMPTZ NOT NULL,
    executed    BOOLEAN DEFAULT false,
    error       TEXT DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_deferred_pending ON deferred_actions(execute_at) WHERE executed = false;
CREATE INDEX idx_deferred_account ON deferred_actions(account_id);

-- Processed message tracking (prevent re-processing)
CREATE TABLE IF NOT EXISTS processed_messages (
    account_id  BIGINT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    message_uid TEXT NOT NULL,
    rule_id     BIGINT REFERENCES rules(id) ON DELETE SET NULL,
    action      TEXT NOT NULL,
    processed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (account_id, message_uid)
);

CREATE INDEX idx_processed_time ON processed_messages(processed_at DESC);
