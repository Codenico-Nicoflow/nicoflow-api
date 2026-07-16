-- Web Push subscriptions (E-025 / NIC-1580). One row per (user, endpoint); a Pro
-- user may have several (multiple browsers/devices). Row-scoped by user; a deleted
-- user's subscriptions cascade away.
CREATE TABLE push_subscriptions (
    id          TEXT         NOT NULL PRIMARY KEY,
    user_id     TEXT         NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    endpoint    TEXT         NOT NULL,
    p256dh_key  TEXT         NOT NULL,
    auth_key    TEXT         NOT NULL,
    user_agent  TEXT,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    UNIQUE (user_id, endpoint)
);

CREATE INDEX idx_push_subscriptions_user ON push_subscriptions(user_id);
