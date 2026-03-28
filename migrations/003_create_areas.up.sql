CREATE TABLE areas (
    id            TEXT         NOT NULL PRIMARY KEY,
    user_id       TEXT         NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name          VARCHAR(255) NOT NULL,
    color         VARCHAR(7)   NOT NULL DEFAULT '#3B82F6',
    display_order INT          NOT NULL DEFAULT 0,
    created_at    TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_areas_user_id ON areas(user_id);
CREATE UNIQUE INDEX idx_areas_user_name ON areas(user_id, name);
