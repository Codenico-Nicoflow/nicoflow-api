ALTER TABLE users
  DROP CONSTRAINT IF EXISTS users_workdays_range,
  DROP CONSTRAINT IF EXISTS users_day_window_ordered;

ALTER TABLE users
  DROP COLUMN IF EXISTS week_start,
  DROP COLUMN IF EXISTS workdays,
  DROP COLUMN IF EXISTS day_start_hour,
  DROP COLUMN IF EXISTS day_end_hour;
