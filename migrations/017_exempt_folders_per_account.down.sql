-- Revert: move exempt_folders back to tenants table.
ALTER TABLE tenants ADD COLUMN IF NOT EXISTS exempt_folders JSONB NOT NULL DEFAULT '["Archive", "Trash", "Spam", "Drafts"]';

-- Copy the first account's exempt_folders back to the tenant (best effort)
UPDATE tenants t
SET exempt_folders = COALESCE(
    (SELECT a.exempt_folders FROM accounts a WHERE a.tenant_id = t.id LIMIT 1),
    '["Archive", "Trash", "Spam", "Drafts"]'::jsonb
);

ALTER TABLE accounts DROP COLUMN IF EXISTS exempt_folders;
