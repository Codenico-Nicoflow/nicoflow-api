ALTER TABLE notification_preferences
    ADD COLUMN overdue_enabled       BOOLEAN NOT NULL DEFAULT TRUE,
    ADD COLUMN daily_summary_enabled BOOLEAN NOT NULL DEFAULT TRUE,
    ADD COLUMN inbox_nudges_enabled  BOOLEAN NOT NULL DEFAULT TRUE,
    ADD COLUMN before_due_minutes    INTEGER NOT NULL DEFAULT 1440,
    ADD COLUMN after_due_minutes     INTEGER NOT NULL DEFAULT 0;

UPDATE notification_preferences SET daily_summary_enabled = evening_digest_enabled;

ALTER TABLE notification_preferences
    DROP COLUMN morning_digest_enabled,
    DROP COLUMN evening_digest_enabled;
