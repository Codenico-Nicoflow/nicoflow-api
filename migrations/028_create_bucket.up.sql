CREATE TABLE bucket (
    id                TEXT         NOT NULL PRIMARY KEY,
    user_id           TEXT         NOT NULL REFERENCES users(id)    ON DELETE CASCADE,
    content           VARCHAR(500) NOT NULL,
    processing_result VARCHAR(15),
    project_id        TEXT         REFERENCES projects(id) ON DELETE SET NULL,
    created_task_id   TEXT         REFERENCES tasks(id)    ON DELETE SET NULL,
    processed_at      TIMESTAMPTZ,
    created_at        TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_bucket_user_processed ON bucket(user_id, processed_at);
