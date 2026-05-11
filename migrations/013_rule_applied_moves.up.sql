-- Track moves performed by the rules engine to prevent feedback loops.
-- When the watcher detects a move, it checks this table first.
-- If the move is found here, it was rule-driven and should NOT create a new rule.

CREATE TABLE IF NOT EXISTS rule_applied_moves (
    id          BIGSERIAL PRIMARY KEY,
    rule_id     BIGINT NOT NULL REFERENCES rules(id) ON DELETE CASCADE,
    message_id  TEXT NOT NULL,
    account_id  BIGINT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    folder      TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Fast lookup: "was this message moved by a rule recently?"
CREATE INDEX idx_rule_applied_moves_lookup
    ON rule_applied_moves (account_id, message_id, created_at DESC);

-- For cleanup job: delete entries older than 24 hours
CREATE INDEX idx_rule_applied_moves_created
    ON rule_applied_moves (created_at);
