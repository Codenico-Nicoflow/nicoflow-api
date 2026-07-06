ALTER TABLE tasks
    ADD COLUMN due_date DATE;

CREATE INDEX idx_tasks_user_due ON tasks(user_id, due_date);
