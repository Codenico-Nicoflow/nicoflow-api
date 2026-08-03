-- Calendar display preferences (E-051 / NIC-1890).
--
-- These live on `users` rather than in a calendar-only table because week_start
-- is NOT purely a display concern: the notification sweeps and any future
-- "this week" grouping key off a week boundary, and a second source of truth
-- would let the backend and the grid disagree about which week a task is in.
-- They sit alongside timezone/theme/language, which are the same kind of value.
--
-- Defaults reproduce today's behaviour exactly, so an existing user sees no
-- change until they choose otherwise: Monday weeks, all seven days, midnight to
-- midnight.
ALTER TABLE users
  -- 0 = Sunday … 6 = Saturday, matching JS getDay() and Go time.Weekday so no
  -- consumer needs a translation table. Constrained rather than an enum: adding
  -- a value is meaningless here (there are exactly seven days).
  ADD COLUMN IF NOT EXISTS week_start SMALLINT NOT NULL DEFAULT 1
    CONSTRAINT users_week_start_range CHECK (week_start BETWEEN 0 AND 6),

  -- Which days the grid draws. An ARRAY, not a `workdays_only` boolean: the
  -- work week is Mon–Fri in most of Europe but Sun–Thu in Israel, and this app
  -- already ships Hebrew + RTL, so a boolean would be wrong for a primary
  -- locale. Empty is rejected — a calendar with no days is not a view, it is a
  -- blank screen.
  ADD COLUMN IF NOT EXISTS workdays SMALLINT[] NOT NULL DEFAULT '{0,1,2,3,4,5,6}'
    CONSTRAINT users_workdays_nonempty CHECK (cardinality(workdays) BETWEEN 1 AND 7),

  -- First hour drawn, 0–23.
  ADD COLUMN IF NOT EXISTS day_start_hour SMALLINT NOT NULL DEFAULT 0
    CONSTRAINT users_day_start_hour_range CHECK (day_start_hour BETWEEN 0 AND 23),

  -- Last hour drawn, EXCLUSIVE, 1–24. 24 means "through midnight", which is why
  -- the ceiling is 24 and not 23: a user asking for 08:00–00:00 wants the 23:00
  -- row drawn, and an inclusive 23 could not express it.
  ADD COLUMN IF NOT EXISTS day_end_hour SMALLINT NOT NULL DEFAULT 24
    CONSTRAINT users_day_end_hour_range CHECK (day_end_hour BETWEEN 1 AND 24);

-- The window must be non-empty. Separate from the per-column ranges because it
-- is a relationship between two columns, and Postgres cannot express that in a
-- column constraint.
ALTER TABLE users
  ADD CONSTRAINT users_day_window_ordered CHECK (day_start_hour < day_end_hour);

-- Every value in `workdays` must be a real weekday. cardinality() above bounds
-- the length; this bounds the contents.
ALTER TABLE users
  ADD CONSTRAINT users_workdays_range CHECK (workdays <@ ARRAY[0,1,2,3,4,5,6]::SMALLINT[]);
