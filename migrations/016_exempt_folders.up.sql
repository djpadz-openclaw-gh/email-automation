-- Add exempt_folders column to tenants table.
-- Folders listed here will not trigger automatic rule creation when messages are moved to them.
ALTER TABLE tenants ADD COLUMN IF NOT EXISTS exempt_folders JSONB NOT NULL DEFAULT '["Archive", "Trash", "Spam", "Drafts"]';
