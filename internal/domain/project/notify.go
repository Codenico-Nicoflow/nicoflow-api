package project

import (
	"context"
	"encoding/json"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/nicoflow/nicoflow-api/internal/domain/notification"
)

// notifier is the slice of the notification funnel the project service emits
// through. Defined here (the consumer) so project never imports the concrete
// notification service; the real *notification.service satisfies it. A nil
// notifier disables emission (notifications are best-effort, never required).
type notifier interface {
	Create(ctx context.Context, n notification.Notification) (notification.NotificationView, bool, error)
}

// statusCompleted is the project.status value that marks explicit completion.
const statusCompleted = "completed"

// emitProjectCompletedIfTransitioned fires project_completed when this update
// carries status into "completed" from a different prior status — the explicit
// user action, never inferred from task counts. Reopening (leaving completed)
// and re-completing later fires again; each transition gets its own dedupe
// bucket via the update timestamp. Fire-and-forget.
func (s *service) emitProjectCompletedIfTransitioned(ctx context.Context, prevStatus string, p Project) {
	if s.notif == nil {
		return
	}
	if prevStatus == statusCompleted || p.Status != statusCompleted {
		return
	}
	nowKey := time.Now().UTC().Format(time.RFC3339Nano)
	if _, _, err := s.notif.Create(ctx, notification.Notification{
		UserID:    p.UserID,
		Type:      notification.TypeProjectCompleted,
		Title:     "Project complete",
		Body:      p.Name + " is marked complete.",
		Metadata:  meta(map[string]string{"projectId": p.ID}),
		DedupeKey: notification.DedupeProjectCompleted(p.ID, nowKey),
	}); err != nil {
		log.Error().Err(err).Str("project_id", p.ID).Msg("emit project_completed failed")
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
