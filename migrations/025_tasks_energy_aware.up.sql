ALTER TABLE tasks
    ADD COLUMN energy            VARCHAR(15) NOT NULL DEFAULT 'medium',
    ADD COLUMN rolls_over        BOOLEAN     NOT NULL DEFAULT TRUE,
    ADD COLUMN priority          VARCHAR(15) NOT NULL DEFAULT 'medium',
    ADD COLUMN estimated_minutes INT,
    ADD COLUMN url               TEXT,
    ADD COLUMN display_order     INT         NOT NULL DEFAULT 0,
    ADD COLUMN completed_at      TIMESTAMPTZ;

CREATE INDEX idx_tasks_project_order  ON tasks(project_id, display_order);
CREATE INDEX idx_tasks_user_energy    ON tasks(user_id, energy, status);
