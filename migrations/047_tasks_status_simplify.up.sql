-- Simplifies tasks.status to active|done|cancelled. `inbox` and `someday` were
-- never distinct from `active` beyond a label; `missed` was the recurrence
-- engine's own bookkeeping riding on the general-purpose status column.
--
-- occurrence_status is the recurrence engine's dedicated field: it records
-- 'missed' when a recurring occurrence's window closes unfinished, independent
-- of tasks.status (which the reap also sets to 'cancelled' so the task drops
-- out of every active-task view/count/search the same way a user cancellation
-- would). Only ever set on rows with recurrence_rule_id IS NOT NULL.
ALTER TABLE tasks ADD COLUMN occurrence_status VARCHAR(63);

UPDATE tasks SET occurrence_status = 'missed', status = 'cancelled' WHERE status = 'missed';
UPDATE tasks SET status = 'active' WHERE status IN ('inbox', 'someday');

ALTER TABLE tasks ADD CONSTRAINT tasks_status_check CHECK (status IN ('active', 'done', 'cancelled'));
