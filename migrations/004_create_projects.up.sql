CREATE TABLE projects (
    id            TEXT        NOT NULL PRIMARY KEY,
    user_id       TEXT        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    area_id       TEXT        REFERENCES areas(id) ON DELETE SET NULL,
    name          TEXT        NOT NULL,
    status        TEXT        NOT NULL DEFAULT 'active',
    display_order INT         NOT NULL DEFAULT 0,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_projects_user_id ON projects(user_id);
CREATE INDEX idx_projects_area_id ON projects(area_id);
