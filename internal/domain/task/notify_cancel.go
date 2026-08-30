package task

import (
	"context"

	"github.com/rs/zerolog/log"
)

// NotificationCanceller removes any pending notifications tied to a task —
// used when an occurrence no longer needs the reminder it would otherwise fire
// (e.g. Skip, NIC-1997). Defined here (the consumer) so task never imports the
// concrete notification service; a nil canceller disables cancellation
// (best-effort, never required).
type NotificationCanceller interface {
	CancelForTask(ctx context.Context, userID, taskID string) error
}

// cancelTaskNotifications removes any notifications tied to t, best-effort: a
// failure is logged and swallowed, exactly like the notify.go emitters — a
// notification-cleanup failure must never fail the caller's mutation.
func (s *service) cancelTaskNotifications(ctx context.Context, userID, taskID string) {
	if s.notifCanceller == nil {
		return
	}
	if err := s.notifCanceller.CancelForTask(ctx, userID, taskID); err != nil {
		log.Error().Err(err).Str("task_id", taskID).Msg("cancel task notifications failed")
	}
}
