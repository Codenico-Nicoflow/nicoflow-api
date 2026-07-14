-- Per-user notification preferences (E-020, NIC-1522). One row per user, lazily
-- created on first PUT — an absent row means "all defaults", so the read path
-- never needs a row to exist. PK = user_id keeps it row-scoped and 1:1 with users.
--
-- email_digest + before_due_minutes are LIVE: consumed by the due-date cron sweep
-- to decide who gets a reminder and how far ahead. push_enabled / sms_enabled
-- persist but are INERT at MVP — no delivery path until E-037.
--
-- Note: users.timezone (IANA, DEFAULT 'UTC') already exists from migration 001,
-- so this story adds no timezone column — AC4 is already satisfied there.
CREATE TABLE notification_preferences (
    user_id            TEXT         NOT NULL PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    email_digest       BOOLEAN      NOT NULL DEFAULT TRUE,
    push_enabled       BOOLEAN      NOT NULL DEFAULT FALSE,
    sms_enabled        BOOLEAN      NOT NULL DEFAULT FALSE,
    before_due_minutes INTEGER      NOT NULL DEFAULT 1440,
    after_due_minutes  INTEGER      NOT NULL DEFAULT 0,
    updated_at         TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);
