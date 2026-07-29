DROP INDEX IF EXISTS idx_tasks_user_scheduled_range;
ALTER TABLE tasks DROP COLUMN IF EXISTS scheduled_time;
