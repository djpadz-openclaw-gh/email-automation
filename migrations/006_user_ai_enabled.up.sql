-- Add ai_enabled flag to users to allow admins to globally disable AI for specific users.
-- Defaults to true for backward compatibility (existing users retain AI access).
ALTER TABLE users ADD COLUMN IF NOT EXISTS ai_enabled BOOLEAN NOT NULL DEFAULT true;
