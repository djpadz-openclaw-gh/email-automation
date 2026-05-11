-- Move exempt_folders from tenant-level to per-account.
-- Each account gets its own exempt folders list, initialized from the tenant's current value.

ALTER TABLE accounts ADD COLUMN IF NOT EXISTS exempt_folders JSONB NOT NULL DEFAULT '["Archive", "Trash", "Spam", "Drafts"]';

-- Migrate existing tenant exempt_folders to all accounts belonging to that tenant
UPDATE accounts a
SET exempt_folders = t.exempt_folders
FROM tenants t
WHERE a.tenant_id = t.id;

-- Drop the column from tenants (no longer needed at tenant level)
ALTER TABLE tenants DROP COLUMN IF EXISTS exempt_folders;
