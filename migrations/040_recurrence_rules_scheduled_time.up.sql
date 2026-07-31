-- Timed recurring tasks. The time lives on the RULE, not on each occurrence:
-- "every weekday at 09:00" is a property of the habit, so every materialized
-- task inherits it and a rule edit re-stamps the live instance — the same
-- template relationship title/priority/energy already have.
--
-- Mirrors tasks.scheduled_time (migration 039) exactly: a nullable bare TIME,
-- NULL meaning "all-day", which is what every pre-existing rule already meant.
ALTER TABLE recurrence_rules ADD COLUMN scheduled_time TIME;
