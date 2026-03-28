CREATE TABLE webhook_events (
    id                     TEXT         NOT NULL PRIMARY KEY,
    lemon_squeezy_event_id VARCHAR(255) NOT NULL UNIQUE,
    event_name             VARCHAR(100) NOT NULL,
    payload                JSONB        NOT NULL,
    processed_at           TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    error                  TEXT
);

