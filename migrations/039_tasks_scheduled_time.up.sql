-- Timed scheduling (E-051 / NIC-1779). `scheduled_time` is a nullable TIME kept
-- deliberately SEPARATE from the existing `scheduled_for` date rather than being
-- folded into a TIMESTAMPTZ: every date-keyed path (notification sweeps,
-- recurrence dedupe, Time Spread placement) reads `scheduled_for` and must keep
-- reading exactly the same column. A nullable add is also the whole data
-- migration — every pre-existing row is valid with scheduled_time NULL, meaning
-- "all-day", which is what those rows already meant.
--
-- No cross-midnight: a task's time belongs to its scheduled_for day, so the
-- service clamps start+estimate at 23:59. Storing a bare TIME (not an interval
-- or an end timestamp) makes that invariant unrepresentable-to-violate here.
ALTER TABLE tasks ADD COLUMN scheduled_time TIME;

-- The calendar's only read pattern: one user's tasks across a date range,
-- returned in grid order. Ordering all-day chips before timed blocks is a
-- product rule (NULLS FIRST on an ASC index), so the index carries it and the
-- ranged query is a plain index scan with no sort.
--
-- scheduled_for is a VARCHAR holding an ISO date, so the range is compared as a
-- string — ISO sorts lexicographically exactly as it does chronologically.
CREATE INDEX idx_tasks_user_scheduled_range
    ON tasks (user_id, scheduled_for, scheduled_time NULLS FIRST, display_order)
    WHERE scheduled_for IS NOT NULL;
