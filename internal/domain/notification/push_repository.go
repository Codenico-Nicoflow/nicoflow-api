package notification

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// UpsertPushSubscription stores a subscription. The conflict target depends on the
// platform, so web and expo rows take separate statements (a single ON CONFLICT
// cannot name three different keys).
func (r *pgRepository) UpsertPushSubscription(ctx context.Context, userID string, sub PushSubscription) error {
	if sub.Platform == PlatformExpo {
		return r.upsertExpoSubscription(ctx, userID, sub)
	}

	// Web: upsert on (user_id, endpoint) so a repeat subscribe from the same
	// browser refreshes the keys instead of duplicating.
	_, err := r.db.Exec(ctx, `
		INSERT INTO push_subscriptions (id, user_id, platform, endpoint, p256dh_key, auth_key, user_agent)
		VALUES (@id, @userID, 'web', @endpoint, @p256dh, @auth, @userAgent)
		ON CONFLICT (user_id, endpoint)
		DO UPDATE SET p256dh_key = EXCLUDED.p256dh_key,
		              auth_key   = EXCLUDED.auth_key,
		              user_agent = EXCLUDED.user_agent`,
		pgx.NamedArgs{
			"id":        uuid.New().String(),
			"userID":    userID,
			"endpoint":  sub.Endpoint,
			"p256dh":    sub.P256dhKey,
			"auth":      sub.AuthKey,
			"userAgent": nullIfEmpty(sub.UserAgent),
		},
	)
	if err != nil {
		return fmt.Errorf("notification.UpsertPushSubscription: %w", err)
	}
	return nil
}

// upsertExpoSubscription keys on device_id when the client supplies one — a device
// gets a fresh Expo token on reinstall/restore, so device_id is the stable identity
// and keying on the token alone would accumulate a dead row per reinstall. Installs
// without a device_id fall back to the token. Both are partial unique indexes, so
// each needs its own conflict target.
func (r *pgRepository) upsertExpoSubscription(ctx context.Context, userID string, sub PushSubscription) error {
	args := pgx.NamedArgs{
		"id":        uuid.New().String(),
		"userID":    userID,
		"token":     sub.ExpoPushToken,
		"deviceID":  nullIfEmpty(sub.DeviceID),
		"userAgent": nullIfEmpty(sub.UserAgent),
	}

	stmt := `
		INSERT INTO push_subscriptions (id, user_id, platform, expo_push_token, device_id, user_agent)
		VALUES (@id, @userID, 'expo', @token, @deviceID, @userAgent)
		ON CONFLICT (user_id, expo_push_token) WHERE platform = 'expo' AND device_id IS NULL
		DO UPDATE SET user_agent = EXCLUDED.user_agent`

	if sub.DeviceID != "" {
		stmt = `
		INSERT INTO push_subscriptions (id, user_id, platform, expo_push_token, device_id, user_agent)
		VALUES (@id, @userID, 'expo', @token, @deviceID, @userAgent)
		ON CONFLICT (user_id, device_id) WHERE platform = 'expo' AND device_id IS NOT NULL
		DO UPDATE SET expo_push_token = EXCLUDED.expo_push_token,
		              user_agent      = EXCLUDED.user_agent`
	}

	if _, err := r.db.Exec(ctx, stmt, args); err != nil {
		return fmt.Errorf("notification.UpsertPushSubscription (expo): %w", err)
	}
	return nil
}

// DeleteExpoPushSubscription removes a user's Expo subscription by token. Idempotent.
func (r *pgRepository) DeleteExpoPushSubscription(ctx context.Context, userID, token string) error {
	_, err := r.db.Exec(ctx, `
		DELETE FROM push_subscriptions
		WHERE user_id = @userID AND platform = 'expo' AND expo_push_token = @token`,
		pgx.NamedArgs{"userID": userID, "token": token},
	)
	if err != nil {
		return fmt.Errorf("notification.DeleteExpoPushSubscription: %w", err)
	}
	return nil
}

// DeletePushSubscription removes a user's subscription by endpoint. Idempotent.
func (r *pgRepository) DeletePushSubscription(ctx context.Context, userID, endpoint string) error {
	_, err := r.db.Exec(ctx, `
		DELETE FROM push_subscriptions WHERE user_id = @userID AND endpoint = @endpoint`,
		pgx.NamedArgs{"userID": userID, "endpoint": endpoint},
	)
	if err != nil {
		return fmt.Errorf("notification.DeletePushSubscription: %w", err)
	}
	return nil
}

// ListPushSubscriptions returns all of a user's stored subscriptions.
func (r *pgRepository) ListPushSubscriptions(ctx context.Context, userID string) ([]PushSubscription, error) {
	rows, err := r.db.Query(ctx, `
		SELECT platform,
		       COALESCE(endpoint, ''), COALESCE(p256dh_key, ''), COALESCE(auth_key, ''),
		       COALESCE(user_agent, ''), COALESCE(expo_push_token, ''), COALESCE(device_id, '')
		FROM push_subscriptions WHERE user_id = @userID`,
		pgx.NamedArgs{"userID": userID},
	)
	if err != nil {
		return nil, fmt.Errorf("notification.ListPushSubscriptions: %w", err)
	}
	defer rows.Close()

	var out []PushSubscription
	for rows.Next() {
		var s PushSubscription
		if err := rows.Scan(&s.Platform, &s.Endpoint, &s.P256dhKey, &s.AuthKey, &s.UserAgent,
			&s.ExpoPushToken, &s.DeviceID); err != nil {
			return nil, fmt.Errorf("notification.ListPushSubscriptions scan: %w", err)
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// nullIfEmpty maps an empty string to nil so the nullable user_agent column stores
// NULL rather than an empty string.
func nullIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
