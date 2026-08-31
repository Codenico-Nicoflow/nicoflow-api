-- Notification rework: task_due_soon, task_overdue, day_plan_nudge,
-- inbox_unprocessed, inbox_stale, task_scheduled_today, and daily_summary are
-- retired in favor of two unified, all-plan digests (morning_digest,
-- evening_digest) that fold their counts together. Fewer, richer pings.
--
-- evening_digest_enabled is backfilled from daily_summary_enabled so an
-- existing opt-out carries over; morning_digest_enabled has no prior
-- equivalent (the old morning outputs were never toggle-gated) and defaults
-- TRUE for everyone. streaks_enabled is untouched — streak_milestone stays a
-- separate Pro perk.
ALTER TABLE notification_preferences
    ADD COLUMN morning_digest_enabled BOOLEAN NOT NULL DEFAULT TRUE,
    ADD COLUMN evening_digest_enabled BOOLEAN NOT NULL DEFAULT TRUE;

UPDATE notification_preferences SET evening_digest_enabled = daily_summary_enabled;

ALTER TABLE notification_preferences
    DROP COLUMN overdue_enabled,
    DROP COLUMN daily_summary_enabled,
    DROP COLUMN inbox_nudges_enabled,
    DROP COLUMN before_due_minutes,
    DROP COLUMN after_due_minutes;
