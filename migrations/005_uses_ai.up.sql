-- Add uses_ai flag to rules to indicate whether a rule employs AI (kiro.*) calls.
-- Auto-detected from Lua code on save; helps users understand AI dependency.
ALTER TABLE rules ADD COLUMN IF NOT EXISTS uses_ai BOOLEAN NOT NULL DEFAULT false;
