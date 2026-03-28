CREATE TABLE tasks (
    id            TEXT        NOT NULL PRIMARY KEY,
    user_id       TEXT        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    project_id    TEXT        REFERENCES projects(id) ON DELETE SET NULL,
    title         TEXT        NOT NULL,
    notes         TEXT        NOT NULL DEFAULT '',
    due_date      TEXT,
    scheduled_for TEXT,
    status        TEXT        NOT NULL DEFAULT 'inbox',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_tasks_user_id ON tasks(user_id);
CREATE INDEX idx_tasks_project_id ON tasks(project_id);
CREATE INDEX idx_tasks_user_status ON tasks(user_id, status);
