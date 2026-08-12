-- tasks.project_id was ON DELETE SET NULL, so deleting a project silently
-- orphaned its tasks instead of removing them (SPEC's "delete a project and
-- all its tasks" was never actually true). Orphaned rows also crashed
-- task.ListActiveByUser: Task.ProjectID is a non-nullable string, and pgx
-- can't scan NULL into it. Switching to CASCADE makes deletion match the
-- documented behavior and removes the NULL entirely, so no Go type change
-- is needed.
DELETE FROM tasks WHERE project_id IS NULL;

ALTER TABLE tasks DROP CONSTRAINT tasks_project_id_fkey;
ALTER TABLE tasks ADD CONSTRAINT tasks_project_id_fkey
    FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE;

ALTER TABLE tasks ALTER COLUMN project_id SET NOT NULL;
