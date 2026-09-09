-- Mobile push (E-037 / NIC-1991). Expo delivers to iOS/Android through its own
-- push service, not Web Push, so a subscription row now carries which transport
-- it belongs to and the Expo token when that transport is Expo.
--
-- Additive and backward compatible: every existing row is Web Push, so platform
-- defaults to 'web' and the existing (user_id, endpoint) uniqueness is untouched.
-- p256dh_key/auth_key/endpoint are Web-Push-only and become nullable, since an
-- Expo row has none of them; the service enforces per-platform requirements.
ALTER TABLE push_subscriptions
    ADD COLUMN platform         TEXT NOT NULL DEFAULT 'web',
    ADD COLUMN expo_push_token  TEXT,
    ADD COLUMN device_id        TEXT;

ALTER TABLE push_subscriptions
    ALTER COLUMN endpoint   DROP NOT NULL,
    ALTER COLUMN p256dh_key DROP NOT NULL,
    ALTER COLUMN auth_key   DROP NOT NULL;

ALTER TABLE push_subscriptions
    ADD CONSTRAINT push_subscriptions_platform_check
        CHECK (platform IN ('web', 'expo'));

-- Each transport must carry the fields its sender needs, and only those. Enforced
-- in the database as well as the service so a bad row cannot be written at all.
ALTER TABLE push_subscriptions
    ADD CONSTRAINT push_subscriptions_shape_check
        CHECK (
            (platform = 'web'  AND endpoint IS NOT NULL AND p256dh_key IS NOT NULL AND auth_key IS NOT NULL)
            OR
            (platform = 'expo' AND expo_push_token IS NOT NULL)
        );

-- Upsert key for Expo rows. A device re-registering gets a fresh token, so the
-- stable identity is device_id when the client sends one; installs that don't
-- have one fall back to the token itself. Partial unique indexes (not table
-- constraints) so web rows are unaffected.
CREATE UNIQUE INDEX idx_push_subscriptions_expo_device
    ON push_subscriptions(user_id, device_id)
    WHERE platform = 'expo' AND device_id IS NOT NULL;

CREATE UNIQUE INDEX idx_push_subscriptions_expo_token
    ON push_subscriptions(user_id, expo_push_token)
    WHERE platform = 'expo' AND device_id IS NULL;
