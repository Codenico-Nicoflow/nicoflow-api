DROP INDEX IF EXISTS idx_projects_user_due_date;
DROP INDEX IF EXISTS idx_projects_user_favorite;

ALTER TABLE projects
    DROP COLUMN IF EXISTS description,
    DROP COLUMN IF EXISTS is_favorite,
    DROP COLUMN IF EXISTS due_date;

ALTER TABLE areas DROP COLUMN IF EXISTS icon;
