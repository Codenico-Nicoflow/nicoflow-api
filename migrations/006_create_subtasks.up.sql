CREATE TABLE subtasks (
    id         TEXT        NOT NULL PRIMARY KEY,
    task_id    TEXT        NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    title      TEXT        NOT NULL,
    done       BOOLEAN     NOT NULL DEFAULT FALSE,
    position   INT         NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_subtasks_task_id ON subtasks(task_id);
