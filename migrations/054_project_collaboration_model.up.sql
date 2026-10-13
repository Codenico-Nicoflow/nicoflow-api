-- NIC-2022: persistence foundation for Project collaboration and lifecycle.
-- Existing Notes are intentionally removed in this pre-launch reset. Take a
-- database snapshot before applying; the down migration cannot restore Note
-- content once this migration has run.

ALTER TABLE areas
    ADD COLUMN archived_at TIMESTAMPTZ,
    ADD COLUMN version BIGINT NOT NULL DEFAULT 1;

ALTER TABLE projects
    ADD COLUMN archived_at TIMESTAMPTZ,
    ADD COLUMN status_before_archive TEXT,
    ADD COLUMN version BIGINT NOT NULL DEFAULT 1,
    ADD COLUMN target_date DATE;

UPDATE projects SET target_date = due_date WHERE due_date IS NOT NULL;

-- Legacy archived Projects have no recorded prior state or archive timestamp.
-- Preserve their archived status, restore them to active if later requested,
-- and start the review clock from this migration rather than inferring an age.
UPDATE projects
SET archived_at = NOW(), status_before_archive = 'active'
WHERE status = 'archived';

ALTER TABLE projects
    ADD CONSTRAINT projects_status_before_archive_check
        CHECK (status_before_archive IS NULL OR status_before_archive IN ('active', 'completed'));

CREATE INDEX idx_areas_user_archive_order
    ON areas(user_id, archived_at, display_order);

CREATE INDEX idx_projects_owner_archive_status
    ON projects(user_id, archived_at, status);

CREATE TABLE project_memberships (
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    joined_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (project_id, user_id)
);

CREATE INDEX idx_project_memberships_user_project
    ON project_memberships(user_id, project_id);

CREATE TABLE project_invitations (
    id              TEXT NOT NULL PRIMARY KEY,
    project_id      TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    inviter_user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    invitee_user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    status          TEXT NOT NULL DEFAULT 'pending'
                    CHECK (status IN ('pending', 'accepted', 'declined', 'withdrawn')),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    responded_at    TIMESTAMPTZ,
    CONSTRAINT project_invitations_no_self_invite
        CHECK (inviter_user_id <> invitee_user_id),
    CONSTRAINT project_invitations_response_time_check
        CHECK ((status = 'pending' AND responded_at IS NULL)
            OR (status <> 'pending' AND responded_at IS NOT NULL))
);

CREATE UNIQUE INDEX idx_project_invitations_pending_unique
    ON project_invitations(project_id, invitee_user_id)
    WHERE status = 'pending';

CREATE INDEX idx_project_invitations_invitee_status
    ON project_invitations(invitee_user_id, status, created_at DESC);

CREATE INDEX idx_project_invitations_project_status
    ON project_invitations(project_id, status, created_at DESC);

ALTER TABLE tasks
    ADD COLUMN assignee_user_id TEXT REFERENCES users(id) ON DELETE SET NULL,
    ADD COLUMN assignee_display_name_snapshot TEXT,
    ADD COLUMN version BIGINT NOT NULL DEFAULT 1,
    ADD COLUMN created_by_user_id TEXT REFERENCES users(id) ON DELETE SET NULL;

CREATE INDEX idx_tasks_project_assignee_status
    ON tasks(project_id, assignee_user_id, status);

-- This is an approved pre-launch reset: note_links cascade from Notes. The
-- bucket's created_note_id is SET NULL by its existing FK.
DELETE FROM notes;

ALTER TABLE notes
    ALTER COLUMN project_id SET NOT NULL;

ALTER TABLE notes
    DROP CONSTRAINT notes_project_id_fkey,
    ADD CONSTRAINT notes_project_id_fkey
        FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE;
