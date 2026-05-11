-- Auto-learned/suggested rules from message move detection
-- These are rules generated automatically when messages are moved between folders.

-- Add source column to rules to distinguish auto-learned from manual
ALTER TABLE rules ADD COLUMN IF NOT EXISTS source TEXT NOT NULL DEFAULT 'manual';
-- source values: 'manual', 'auto-learned'

-- Add approved column - auto-learned rules start as unapproved
ALTER TABLE rules ADD COLUMN IF NOT EXISTS approved BOOLEAN NOT NULL DEFAULT true;

-- Track known message locations for move detection
CREATE TABLE IF NOT EXISTS message_locations (
    account_id  BIGINT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    message_uid TEXT NOT NULL,
    folder      TEXT NOT NULL,
    message_id  TEXT DEFAULT '',
    sender      TEXT DEFAULT '',
    subject     TEXT DEFAULT '',
    seen_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (account_id, message_uid, folder)
);

CREATE INDEX idx_message_locations_account ON message_locations(account_id);
CREATE INDEX idx_message_locations_seen ON message_locations(seen_at);

-- Track detected moves for auto-learning
CREATE TABLE IF NOT EXISTS detected_moves (
    id          BIGSERIAL PRIMARY KEY,
    account_id  BIGINT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    message_uid TEXT NOT NULL,
    message_id  TEXT DEFAULT '',
    sender      TEXT DEFAULT '',
    subject     TEXT DEFAULT '',
    from_folder TEXT NOT NULL,
    to_folder   TEXT NOT NULL,
    detected_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    rule_id     BIGINT REFERENCES rules(id) ON DELETE SET NULL  -- generated rule, if any
);

CREATE INDEX idx_detected_moves_account ON detected_moves(account_id);
CREATE INDEX idx_detected_moves_time ON detected_moves(detected_at DESC);
