-- Email metadata: key-value tags attached to emails by rules or API
CREATE TABLE IF NOT EXISTS email_metadata (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    message_id TEXT NOT NULL,
    key VARCHAR(255) NOT NULL,
    value TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(tenant_id, message_id, key)
);

-- Fast lookup by tenant + key + value (for querying emails by metadata)
CREATE INDEX idx_email_metadata_lookup ON email_metadata (tenant_id, key, value);

-- Fast lookup by message_id within a tenant
CREATE INDEX idx_email_metadata_message ON email_metadata (tenant_id, message_id);
