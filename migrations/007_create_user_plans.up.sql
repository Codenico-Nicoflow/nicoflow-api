CREATE TABLE user_plans (
    id                            TEXT        NOT NULL PRIMARY KEY,
    user_id                       TEXT        NOT NULL UNIQUE REFERENCES users(id) ON DELETE CASCADE,
    plan                          TEXT        NOT NULL DEFAULT 'free',
    status                        TEXT        NOT NULL DEFAULT 'active',
    lemon_squeezy_subscription_id TEXT        NOT NULL DEFAULT '',
    lemon_squeezy_customer_id     TEXT        NOT NULL DEFAULT '',
    current_period_start          TIMESTAMPTZ,
    current_period_end            TIMESTAMPTZ,
    created_at                    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at                    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
