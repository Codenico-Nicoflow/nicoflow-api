-- File attachments (E-024 / NIC-1638). Polymorphic owner: (owner_type, owner_id)
-- lets tasks now and notes later share one table with no schema rework. There is
-- deliberately NO FK on the owner columns — cleanup on owner delete is explicit
-- (the BE-4 GC sweep), because a polymorphic owner can't be a single FK target.
-- user_id DOES cascade: a hard-deleted user takes their attachment rows with them
-- (the S3 objects are reclaimed separately by the sweep).
CREATE TABLE file_attachments (
    id          TEXT         NOT NULL PRIMARY KEY,
    owner_type  TEXT         NOT NULL,
    owner_id    TEXT         NOT NULL,
    user_id     TEXT         NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    file_name   TEXT         NOT NULL,
    file_size   BIGINT       NOT NULL,
    mime_type   TEXT         NOT NULL,
    s3_key      TEXT         NOT NULL UNIQUE,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_file_attachments_owner ON file_attachments(owner_type, owner_id);
CREATE INDEX idx_file_attachments_user ON file_attachments(user_id);
