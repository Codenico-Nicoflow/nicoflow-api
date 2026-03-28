CREATE TABLE ai_sessions (
    id         TEXT         NOT NULL PRIMARY KEY,
    user_id    TEXT         NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    title      VARCHAR(255) NOT NULL DEFAULT 'New Conversation',
    status     VARCHAR(31)  NOT NULL DEFAULT 'active',
    created_at TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_ai_sessions_user_id ON ai_sessions(user_id);
