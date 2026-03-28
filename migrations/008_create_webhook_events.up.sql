CREATE TABLE webhook_events (
    id                     TEXT        NOT NULL PRIMARY KEY,
    lemon_squeezy_event_id TEXT        NOT NULL UNIQUE,
    event_name             TEXT        NOT NULL,
    payload                JSONB       NOT NULL,
    processed_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    error                  TEXT        NOT NULL DEFAULT ''
);

CREATE INDEX idx_webhook_events_ls_event_id ON webhook_events(lemon_squeezy_event_id);
