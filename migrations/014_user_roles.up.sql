-- Add role column to users table for RBAC
-- Roles: 'user' (default), 'admin'

ALTER TABLE users ADD COLUMN IF NOT EXISTS role TEXT NOT NULL DEFAULT 'user';

-- User #1 is always admin
UPDATE users SET role = 'admin' WHERE id = 1;
