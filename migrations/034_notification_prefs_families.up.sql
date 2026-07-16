-- Per-family notification toggles (E-025, NIC-1591). Each proactive sweep gains an
-- independent on/off so users can silence a family without disabling the rest.
-- All default TRUE — existing users keep receiving every family until they opt out
-- (matches the pre-migration behaviour where the sweeps had no toggle at all).
--
--   overdue_enabled       → the overdue-task reminder sweep (NIC-1566)
--   daily_summary_enabled → the end-of-day completion summary (NIC-1572)
--   inbox_nudges_enabled  → the inbox unprocessed/stale nudges (NIC-1571)
--   streaks_enabled       → the streak-milestone celebration (NIC-1572)
--
-- An absent preferences row still means "all defaults" (all families on) — the
-- sweep queries COALESCE these to TRUE, so no row need exist.
ALTER TABLE notification_preferences
    ADD COLUMN overdue_enabled       BOOLEAN NOT NULL DEFAULT TRUE,
    ADD COLUMN daily_summary_enabled BOOLEAN NOT NULL DEFAULT TRUE,
    ADD COLUMN inbox_nudges_enabled  BOOLEAN NOT NULL DEFAULT TRUE,
    ADD COLUMN streaks_enabled       BOOLEAN NOT NULL DEFAULT TRUE;
