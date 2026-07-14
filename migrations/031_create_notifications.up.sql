CREATE TABLE notifications (
    id          TEXT         NOT NULL PRIMARY KEY,
    user_id     TEXT         NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    type        TEXT         NOT NULL,
    title       TEXT         NOT NULL,
    body        TEXT         NOT NULL DEFAULT '',
    metadata    JSONB        NOT NULL DEFAULT '{}',
    is_read     BOOLEAN      NOT NULL DEFAULT FALSE,
    read_at     TIMESTAMPTZ,
    dedupe_key  TEXT,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

-- Idempotency: a producer sets dedupe_key (e.g. task_due_soon:<taskId>:<YYYY-MM-DD>)
-- so re-running generation is a no-op via ON CONFLICT DO NOTHING. NULL keys never
-- collide (Postgres treats NULLs as distinct in a unique index).
CREATE UNIQUE INDEX uq_notifications_user_dedupe
    ON notifications(user_id, dedupe_key)
    WHERE dedupe_key IS NOT NULL;

-- Drives the list (keyset on created_at, id) and the unread-count filter.
CREATE INDEX idx_notifications_user_read_created
    ON notifications(user_id, is_read, created_at DESC, id DESC);
