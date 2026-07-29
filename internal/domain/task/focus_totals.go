package task

import (
	"context"
	"fmt"
)

// FocusTotals is the narrow slice of the focus domain the task service needs:
// how many seconds of closed focus segments has a task accrued? Defined here
// (the consumer) and satisfied by focus.Repository, so task never imports focus
// — the concretes meet only in main.go wiring.
type FocusTotals interface {
	// SumClosedSecondsByTask totals one task's closed segments, 0 when none.
	SumClosedSecondsByTask(ctx context.Context, userID, taskID string) (int64, error)
	// SumClosedSecondsByTaskBatch answers many tasks in one round trip. Tasks
	// with no closed segments are absent from the map — a miss reads as 0.
	SumClosedSecondsByTaskBatch(ctx context.Context, userID string, taskIDs []string) (map[string]int64, error)
}

// enrichFocusTotals fills TotalFocusSeconds on the given views with one batch
// query. A nil FocusTotals is a valid no-op seam — views keep the zero default.
func (s *service) enrichFocusTotals(ctx context.Context, userID string, views []TaskView) error {
	if s.focusTotals == nil || len(views) == 0 {
		return nil
	}
	ids := make([]string, len(views))
	for i, v := range views {
		ids[i] = v.ID
	}
	totals, err := s.focusTotals.SumClosedSecondsByTaskBatch(ctx, userID, ids)
	if err != nil {
		return fmt.Errorf("task focus totals batch: %w", err)
	}
	for i := range views {
		views[i].TotalFocusSeconds = totals[views[i].ID]
	}
	return nil
}
