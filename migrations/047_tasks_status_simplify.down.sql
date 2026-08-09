ALTER TABLE tasks DROP CONSTRAINT tasks_status_check;

UPDATE tasks SET status = 'missed' WHERE occurrence_status = 'missed';

ALTER TABLE tasks DROP COLUMN occurrence_status;
