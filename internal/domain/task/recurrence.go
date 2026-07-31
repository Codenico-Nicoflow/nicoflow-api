package task

import (
	"context"

	"github.com/rs/zerolog/log"
)

// RecurrenceMaterializer creates the successor of a completed recurring
// occurrence. Defined here (the consumer) so the task package never imports the
// recurrence domain; the concrete is the recurrence materializer, injected at
// wire-up in main.go. Nil disables the synchronous trigger — the hourly cron
// sweep still catches the rule on its next tick.
type RecurrenceMaterializer interface {
	MaterializeAfterCompletion(ctx context.Context, userID, ruleID string) error
}

// materializeSuccessor is trigger 2 of the recurrence engine: completing an
// occurrence immediately creates the next one, so an active user sees the habit
// continue without waiting for the cron. Called from the shared update path, so
// every route into `done` — the list checkbox and the edit dialog alike — spawns
// the successor. Best-effort by design — the status change has already
// committed, and the partial unique index makes this racing the sweep safe, so a
// failure is logged and swallowed rather than failing a request the user
// considers done.
func (s *service) materializeSuccessor(ctx context.Context, userID string, view TaskView) {
	if s.materializer == nil || view.RecurrenceRuleID == nil || view.Status != statusDone {
		return
	}
	if err := s.materializer.MaterializeAfterCompletion(ctx, userID, *view.RecurrenceRuleID); err != nil {
		log.Error().Err(err).
			Str("task_id", view.ID).Str("rule_id", *view.RecurrenceRuleID).
			Msg("task: recurrence successor not materialized — cron sweep will retry")
	}
}
