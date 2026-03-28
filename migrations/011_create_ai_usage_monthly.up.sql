CREATE TABLE ai_usage_monthly (
    id            TEXT NOT NULL PRIMARY KEY,
    user_id       TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    month         DATE NOT NULL,
    request_count INT  NOT NULL DEFAULT 0,
    UNIQUE (user_id, month)
);

CREATE INDEX idx_ai_usage_monthly_user_month ON ai_usage_monthly(user_id, month);
