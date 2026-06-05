ALTER TABLE areas ADD COLUMN icon VARCHAR(50) NOT NULL DEFAULT 'folder';

ALTER TABLE projects
    ADD COLUMN due_date    TIMESTAMPTZ,
    ADD COLUMN is_favorite BOOLEAN     NOT NULL DEFAULT FALSE,
    ADD COLUMN description TEXT;

CREATE INDEX idx_projects_user_favorite ON projects(user_id, is_favorite) WHERE is_favorite = TRUE;
CREATE INDEX idx_projects_user_due_date ON projects(user_id, due_date) WHERE due_date IS NOT NULL;
