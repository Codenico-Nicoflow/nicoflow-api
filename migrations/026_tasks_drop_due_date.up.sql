-- Tasks drop the hard deadline (due_date): the calm model keeps a single soft
-- scheduled_for intention. Projects keep their due_date.
DROP INDEX IF EXISTS idx_tasks_user_due;

ALTER TABLE tasks
    DROP COLUMN IF EXISTS due_date;
