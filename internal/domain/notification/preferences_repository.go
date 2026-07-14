package notification

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// GetPreferences returns the user's stored preferences, or the defaults when no
// row exists (an absent row means "all defaults" — never an error).
func (r *pgRepository) GetPreferences(ctx context.Context, userID string) (Preferences, error) {
	var p Preferences
	err := r.db.QueryRow(ctx, `
		SELECT user_id, email_digest, push_enabled, sms_enabled,
		       before_due_minutes, after_due_minutes, updated_at
		FROM notification_preferences
		WHERE user_id = @userID`,
		pgx.NamedArgs{"userID": userID},
	).Scan(&p.UserID, &p.EmailDigest, &p.PushEnabled, &p.SMSEnabled,
		&p.BeforeDueMinutes, &p.AfterDueMinutes, &p.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return defaultPreferences(userID), nil
		}
		return Preferences{}, fmt.Errorf("notification.GetPreferences: %w", err)
	}
	return p, nil
}

// UpsertPreferences lazily creates or partially updates the user's row. On create,
// nil fields land at their column defaults (COALESCE against the default constant);
// on update, nil fields keep the existing stored value. Returns the resulting row.
func (r *pgRepository) UpsertPreferences(ctx context.Context, userID string, u UpdatePreferences) (Preferences, error) {
	var p Preferences
	err := r.db.QueryRow(ctx, `
		INSERT INTO notification_preferences (
			user_id, email_digest, push_enabled, sms_enabled,
			before_due_minutes, after_due_minutes, updated_at
		) VALUES (
			@userID,
			COALESCE(@emailDigest::boolean, @defEmailDigest),
			COALESCE(@pushEnabled::boolean, @defPushEnabled),
			COALESCE(@smsEnabled::boolean, @defSMSEnabled),
			COALESCE(@beforeDueMinutes::integer, @defBeforeDueMinutes),
			COALESCE(@afterDueMinutes::integer, @defAfterDueMinutes),
			NOW()
		)
		ON CONFLICT (user_id) DO UPDATE SET
			email_digest       = COALESCE(@emailDigest::boolean, notification_preferences.email_digest),
			push_enabled       = COALESCE(@pushEnabled::boolean, notification_preferences.push_enabled),
			sms_enabled        = COALESCE(@smsEnabled::boolean, notification_preferences.sms_enabled),
			before_due_minutes = COALESCE(@beforeDueMinutes::integer, notification_preferences.before_due_minutes),
			after_due_minutes  = COALESCE(@afterDueMinutes::integer, notification_preferences.after_due_minutes),
			updated_at         = NOW()
		RETURNING user_id, email_digest, push_enabled, sms_enabled,
		          before_due_minutes, after_due_minutes, updated_at`,
		pgx.NamedArgs{
			"userID":              userID,
			"emailDigest":         u.EmailDigest,
			"pushEnabled":         u.PushEnabled,
			"smsEnabled":          u.SMSEnabled,
			"beforeDueMinutes":    u.BeforeDueMinutes,
			"afterDueMinutes":     u.AfterDueMinutes,
			"defEmailDigest":      DefaultEmailDigest,
			"defPushEnabled":      DefaultPushEnabled,
			"defSMSEnabled":       DefaultSMSEnabled,
			"defBeforeDueMinutes": DefaultBeforeDueMinutes,
			"defAfterDueMinutes":  DefaultAfterDueMinutes,
		},
	).Scan(&p.UserID, &p.EmailDigest, &p.PushEnabled, &p.SMSEnabled,
		&p.BeforeDueMinutes, &p.AfterDueMinutes, &p.UpdatedAt)
	if err != nil {
		return Preferences{}, fmt.Errorf("notification.UpsertPreferences: %w", err)
	}
	return p, nil
}
