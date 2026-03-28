CREATE TABLE user_plans (
    id                            TEXT        NOT NULL PRIMARY KEY,
    user_id                       TEXT        NOT NULL UNIQUE REFERENCES users(id) ON DELETE CASCADE,
    plan                          VARCHAR(20) NOT NULL DEFAULT 'free',
    status                        VARCHAR(20) NOT NULL DEFAULT 'active',
    lemon_squeezy_subscription_id VARCHAR(255),
    lemon_squeezy_customer_id     VARCHAR(255),
    current_period_start          TIMESTAMPTZ,
    current_period_end            TIMESTAMPTZ,
    created_at                    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at                    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_user_plans_user_id ON user_plans(user_id);
