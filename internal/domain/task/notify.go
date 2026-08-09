package task

import (
	"context"
	"encoding/json"
	"time"

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
		Metadata:  meta(map[string]string{"taskId": t.ID, "projectId": t.ProjectID}),
		DedupeKey: notification.DedupeTaskCompleted(t.ID),
	}); err != nil {
		log.Error().Err(err).Str("task_id", t.ID).Msg("emit task_completed failed")
	}
}

// emitProjectCompletedIfLast fires project_completed when completing this task
// leaves the project with zero non-terminal tasks (the 1→0 edge). Deduped once
// per project per local day. Fire-and-forget.
func (s *service) emitProjectCompletedIfLast(ctx context.Context, t Task) {
	if s.notif == nil {
		return
	}
	remaining, err := s.repo.CountNonTerminalByProject(ctx, t.UserID, t.ProjectID)
	if err != nil {
		log.Error().Err(err).Str("project_id", t.ProjectID).Msg("count non-terminal tasks failed")
		return
	}
	if remaining > 0 {
		return
	}
	isoDate := time.Now().UTC().Format(scheduledForLayout)
	if _, _, err := s.notif.Create(ctx, notification.Notification{
		UserID:    t.UserID,
		Type:      notification.TypeProjectCompleted,
		Title:     "Project complete",
		Body:      "Every task in this project is done.",
		Metadata:  meta(map[string]string{"projectId": t.ProjectID}),
		DedupeKey: notification.DedupeProjectCompleted(t.ProjectID, isoDate),
	}); err != nil {
		log.Error().Err(err).Str("project_id", t.ProjectID).Msg("emit project_completed failed")
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
