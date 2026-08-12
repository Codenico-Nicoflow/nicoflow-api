ALTER TABLE tasks ALTER COLUMN project_id DROP NOT NULL;

ALTER TABLE tasks DROP CONSTRAINT tasks_project_id_fkey;
ALTER TABLE tasks ADD CONSTRAINT tasks_project_id_fkey
    FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE SET NULL;
