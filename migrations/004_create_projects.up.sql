CREATE TABLE projects (
    id            TEXT         NOT NULL PRIMARY KEY,
    user_id       TEXT         NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    area_id       TEXT         REFERENCES areas(id) ON DELETE SET NULL,
    name          VARCHAR(255) NOT NULL,
    status        VARCHAR(63)  NOT NULL DEFAULT 'active',
    display_order INT          NOT NULL DEFAULT 0,
    created_at    TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_projects_user_id ON projects(user_id);
CREATE INDEX idx_projects_area_id ON projects(area_id);
CREATE UNIQUE INDEX idx_projects_user_name ON projects(user_id, name);
