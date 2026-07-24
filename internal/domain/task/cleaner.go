package task

import (
	"context"

	"github.com/rs/zerolog/log"
)

// ownerTypeTask is the polymorphic owner-type value tasks register with the
// attachment domain. Kept local so the task package never imports attachment.
const ownerTypeTask = "task"

// AttachmentCleaner removes a deleted owner's attachments. Defined here (the
// consumer) so the task package stays free of any attachment import; the concrete
// is the attachment service, injected at wire-up. Nil disables cleanup (no
// attachment feature), and the concretes meet only in main.go — acyclic, since
// the attachment service already depends on the task repo via OwnerVerifier.
type AttachmentCleaner interface {
	DeleteAllForOwner(ctx context.Context, userID, ownerType, ownerID string) error
}

// cleanAttachments best-effort removes a deleted task's attachments. A nil
// cleaner is a no-op; a failure is logged and swallowed so attachment cleanup
// can never block or fail the task delete that already committed.
func (s *service) cleanAttachments(ctx context.Context, userID, taskID string) {
	if s.cleaner == nil {
		return
	}
	if err := s.cleaner.DeleteAllForOwner(ctx, userID, ownerTypeTask, taskID); err != nil {
		log.Error().Err(err).Str("task_id", taskID).Msg("task: attachment cleanup failed — task deleted anyway")
	}
}
