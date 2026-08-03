-- Project Notes (E-053 / NIC-1880). A note is a rich-text document owned by a
-- user and filed under a project.
--
-- project_id is nullable with ON DELETE SET NULL, mirroring tasks.project_id
-- (migration 005): deleting a project ORPHANS its notes rather than destroying
-- them. CASCADE was rejected — notes hold reference material a user would not
-- expect a project delete to take with it. The API still requires projectId on
-- create; nullability exists only to survive the delete.
--
-- content is NOT NULL with an empty-doc default so the frontend never branches
-- on null-vs-empty. content_text is the flattened plain text kept alongside it
-- purely so the search vector has an IMMUTABLE source (see below).
CREATE TABLE notes (
    id            TEXT         NOT NULL PRIMARY KEY,
    user_id       TEXT         NOT NULL REFERENCES users(id)    ON DELETE CASCADE,
    project_id    TEXT         REFERENCES projects(id) ON DELETE SET NULL,
    title         VARCHAR(255) NOT NULL,
    content       JSONB        NOT NULL DEFAULT '{"type":"doc","content":[]}'::jsonb,
    content_text  TEXT         NOT NULL DEFAULT '',
    version       INT          NOT NULL DEFAULT 1,
    created_at    TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_notes_user_id      ON notes(user_id);
CREATE INDEX idx_notes_project_id   ON notes(project_id);
CREATE INDEX idx_notes_user_updated ON notes(user_id, updated_at DESC);

-- The vector is generated from content_text, NOT content: walking a JSONB tree
-- is not IMMUTABLE and so cannot appear in a GENERATED ALWAYS AS expression.
-- 'simple' matches migration 030 (tasks/projects/areas) — no stemming, so
-- prefix/type-ahead search works and every language is treated alike.
ALTER TABLE notes
    ADD COLUMN search_vector tsvector
    GENERATED ALWAYS AS (
        to_tsvector('simple', coalesce(title, '') || ' ' || coalesce(content_text, ''))
    ) STORED;

CREATE INDEX notes_search_idx ON notes USING GIN (search_vector);

-- Processing an inbox item into a note records which note it became, mirroring
-- bucket.created_task_id (migration 028).
ALTER TABLE bucket
    ADD COLUMN created_note_id TEXT REFERENCES notes(id) ON DELETE SET NULL;
