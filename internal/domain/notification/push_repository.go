package notification

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// UpsertPushSubscription stores a subscription, upserting on (user_id, endpoint) so
// a repeat subscribe from the same browser refreshes the keys instead of duplicating.
func (r *pgRepository) UpsertPushSubscription(ctx context.Context, userID string, sub PushSubscription) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO push_subscriptions (id, user_id, endpoint, p256dh_key, auth_key, user_agent)
		VALUES (@id, @userID, @endpoint, @p256dh, @auth, @userAgent)
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
		SELECT endpoint, p256dh_key, auth_key, COALESCE(user_agent, '')
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
		if err := rows.Scan(&s.Endpoint, &s.P256dhKey, &s.AuthKey, &s.UserAgent); err != nil {
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
