-- Add role column to users table for RBAC
-- User #1 is always admin (enforced at application level)
ALTER TABLE users ADD COLUMN IF NOT EXISTS role TEXT NOT NULL DEFAULT 'user';

-- Set user #1 as admin
UPDATE users SET role = 'admin' WHERE id = 1;
