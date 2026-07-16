ALTER TABLE notification_preferences
    DROP COLUMN overdue_enabled,
    DROP COLUMN daily_summary_enabled,
    DROP COLUMN inbox_nudges_enabled,
    DROP COLUMN streaks_enabled;
