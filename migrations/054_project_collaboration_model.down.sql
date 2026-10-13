-- Reverses the schema only. Note content deleted by the up migration is
-- irreversible here; recovery requires restoring the pre-migration DB snapshot.

ALTER TABLE notes
    DROP CONSTRAINT notes_project_id_fkey,
    ALTER COLUMN project_id DROP NOT NULL,
    ADD CONSTRAINT notes_project_id_fkey
        FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE SET NULL;

DROP INDEX IF EXISTS idx_tasks_project_assignee_status;
ALTER TABLE tasks
    DROP COLUMN created_by_user_id,
    DROP COLUMN version,
    DROP COLUMN assignee_display_name_snapshot,
    DROP COLUMN assignee_user_id;

DROP TABLE project_invitations;
DROP TABLE project_memberships;

DROP INDEX IF EXISTS idx_projects_owner_archive_status;
ALTER TABLE projects
    DROP CONSTRAINT projects_status_before_archive_check,
    DROP COLUMN target_date,
    DROP COLUMN version,
    DROP COLUMN status_before_archive,
    DROP COLUMN archived_at;

DROP INDEX IF EXISTS idx_areas_user_archive_order;
ALTER TABLE areas
    DROP COLUMN version,
    DROP COLUMN archived_at;
