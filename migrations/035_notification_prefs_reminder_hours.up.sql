-- Per-user reminder hours (E-025, NIC-1627). Replaces the two hardcoded sweep
-- constants (reminderLocalHour = 8, summaryLocalHour = 20) with configurable
-- columns so users can pick when their morning and evening notifications fire.
--
--   morning_hour → day-start, inbox, overdue, due-notify sweeps (defaults 08:00)
--   evening_hour → the end-of-day summary sweep              (defaults 20:00)
--
-- Ranges are clamped by CHECK so a bad value can never reach a sweep. An absent
-- preferences row still means "all defaults" — ListRemindableUsers COALESCEs
-- these to 8/20, so no row need exist.
ALTER TABLE notification_preferences
    ADD COLUMN morning_hour SMALLINT NOT NULL DEFAULT 8  CHECK (morning_hour BETWEEN 5 AND 11),
    ADD COLUMN evening_hour SMALLINT NOT NULL DEFAULT 20 CHECK (evening_hour BETWEEN 18 AND 22);
