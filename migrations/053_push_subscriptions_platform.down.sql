-- Expo rows have no endpoint/keys, so they cannot survive a revert to the
-- web-only shape — drop them before restoring the NOT NULL constraints.
DELETE FROM push_subscriptions WHERE platform = 'expo';

DROP INDEX IF EXISTS idx_push_subscriptions_expo_token;
DROP INDEX IF EXISTS idx_push_subscriptions_expo_device;

ALTER TABLE push_subscriptions
    DROP CONSTRAINT IF EXISTS push_subscriptions_shape_check,
    DROP CONSTRAINT IF EXISTS push_subscriptions_platform_check;

ALTER TABLE push_subscriptions
    ALTER COLUMN endpoint   SET NOT NULL,
    ALTER COLUMN p256dh_key SET NOT NULL,
    ALTER COLUMN auth_key   SET NOT NULL;

ALTER TABLE push_subscriptions
    DROP COLUMN device_id,
    DROP COLUMN expo_push_token,
    DROP COLUMN platform;
