package task

import (
	"context"
	"encoding/json"

	"github.com/rs/zerolog/log"

	"github.com/nicoflow/nicoflow-api/internal/domain/notification"
)

// notifier is the slice of the notification funnel the task service emits
// through. Defined here (the consumer) so task never imports the concrete
// notification service; the real *notification.service satisfies it. A nil
// notifier disables emission (notifications are best-effort, never required).
type notifier interface {
	Create(ctx context.Context, n notification.Notification) (notification.NotificationView, bool, error)
}

// emitTaskCompleted fires a task_completed notification after a task moves into
// done. Fire-and-forget: any error is logged and swallowed so a notification
// failure can never fail the user's mutation. Deduped once per task.
func (s *service) emitTaskCompleted(ctx context.Context, t Task) {
	if s.notif == nil {
		return
	}
	if _, _, err := s.notif.Create(ctx, notification.Notification{
		UserID:    t.UserID,
		Type:      notification.TypeTaskCompleted,
		Title:     t.Title,
		Body:      "Task completed.",
		Metadata:  meta(map[string]string{"entityType": "task", "entityId": t.ID, "projectId": t.ProjectID}),
		DedupeKey: notification.DedupeTaskCompleted(t.ID),
	}); err != nil {
		log.Error().Err(err).Str("task_id", t.ID).Msg("emit task_completed failed")
	}
}

// meta marshals a small deep-link map; on the (impossible for string maps)
// marshal error it degrades to an empty object rather than failing emission.
func meta(m map[string]string) json.RawMessage {
	b, err := json.Marshal(m)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return b
}
