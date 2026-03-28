CREATE TABLE ai_messages (
    id         TEXT        NOT NULL PRIMARY KEY,
    session_id TEXT        NOT NULL REFERENCES ai_sessions(id) ON DELETE CASCADE,
    role       TEXT        NOT NULL,
    content    TEXT        NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_ai_messages_session_id ON ai_messages(session_id);
