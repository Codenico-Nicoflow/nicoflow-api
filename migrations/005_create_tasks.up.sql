CREATE TABLE tasks (
    id            TEXT         NOT NULL PRIMARY KEY,
    user_id       TEXT         NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    project_id    TEXT         REFERENCES projects(id) ON DELETE SET NULL,
    title         VARCHAR(255) NOT NULL,
    notes         TEXT,
    due_date      DATE,
    scheduled_for VARCHAR(31),
    status        VARCHAR(63)  NOT NULL DEFAULT 'inbox',
    created_at    TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_tasks_user_id        ON tasks(user_id);
CREATE INDEX idx_tasks_project_id     ON tasks(project_id);
CREATE INDEX idx_tasks_user_status    ON tasks(user_id, status);
CREATE INDEX idx_tasks_user_scheduled ON tasks(user_id, scheduled_for);
CREATE INDEX idx_tasks_user_due       ON tasks(user_id, due_date);
