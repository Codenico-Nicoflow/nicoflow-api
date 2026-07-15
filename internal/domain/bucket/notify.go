package bucket

import (
	"context"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/nicoflow/nicoflow-api/internal/domain/notification"
)

// isoDateLayout is the local-date bucket for the once-per-day inbox_zero dedupe.
const isoDateLayout = "2006-01-02"

// notifier is the slice of the notification funnel the bucket service emits
// through. Defined here (the consumer) so bucket never imports the concrete
// notification service. A nil notifier disables emission (best-effort).
type notifier interface {
	Create(ctx context.Context, n notification.Notification) (notification.NotificationView, bool, error)
}

// maybeEmitInboxZero fires inbox_zero when the user has just cleared their last
// unprocessed item. Pro-only (skipped for free users) and deduped once per user
// per local day. Fire-and-forget: a notification failure never fails the
// mutation that triggered it.
func (s *service) maybeEmitInboxZero(ctx context.Context, userID, plan string) {
	if s.notif == nil || plan != planPro {
		return
	}
	remaining, err := s.repo.CountUnprocessed(ctx, userID)
	if err != nil {
		log.Error().Err(err).Str("user_id", userID).Msg("count unprocessed failed")
		return
	}
	if remaining > 0 {
		return
	}
	isoDate := time.Now().UTC().Format(isoDateLayout)
	if _, _, err := s.notif.Create(ctx, notification.Notification{
		UserID:    userID,
		Type:      notification.TypeInboxZero,
		Title:     "Inbox zero",
		Body:      "You've processed everything in your inbox.",
		DedupeKey: notification.DedupeInboxZero(userID, isoDate),
	}); err != nil {
		log.Error().Err(err).Str("user_id", userID).Msg("emit inbox_zero failed")
	}
}
