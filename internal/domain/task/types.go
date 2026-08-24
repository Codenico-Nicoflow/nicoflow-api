// Package task contains the task and subtask domain.
package task

import (
	"time"

	"github.com/nicoflow/nicoflow-api/pkg/optional"
)

// Task is the internal domain model for a task.
type Task struct {
	ID           string
	UserID       string
	ProjectID    string
	Title        string
	Notes        *string
	Status       string
	Priority     string
	Energy       string
	RollsOver    bool
	ScheduledFor *string
	// ScheduledTime is the optional time-of-day ("HH:MM") on the scheduled_for
	// day (E-051). Nil = all-day, which is what every pre-timed task means.
	ScheduledTime    *string
	EstimatedMinutes *int
	URL              *string
	DisplayOrder     int
	CompletedAt      *time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
	// RecurrenceRuleID / OccurrenceDate are set only on materialized recurring
	// occurrences (E-050). Both nil on an ordinary task.
	RecurrenceRuleID *string
	OccurrenceDate   *string
	// OccurrenceStatus is nil on an ordinary or still-live occurrence; "missed"
	// once the recurrence engine has reaped it (E-050 sweep or manual mark).
	OccurrenceStatus *string
	// SubtaskCount / OpenSubtaskCount are read-only projections filled by every
	// task read (see taskSelectCols). OpenSubtaskCount > 0 is what makes the
	// client confirm before completing a task.
	SubtaskCount     int
	OpenSubtaskCount int
}

// TaskView is the JSON response shape (ITask) for a single task.
type TaskView struct {
	ID               string  `json:"id"`
	ProjectID        string  `json:"projectId"`
	Title            string  `json:"title"`
	Notes            *string `json:"notes"`
	Status           string  `json:"status"`
	Priority         string  `json:"priority"`
	Energy           string  `json:"energy"`
	RollsOver        bool    `json:"rollsOver"`
	ScheduledFor     *string `json:"scheduledFor"`
	ScheduledTime    *string `json:"scheduledTime"`
	EstimatedMinutes *int    `json:"estimatedMinutes"`
	URL              *string `json:"url"`
	DisplayOrder     int     `json:"displayOrder"`
	CompletedAt      *string `json:"completedAt"`
	CreatedAt        string  `json:"createdAt"`
	UpdatedAt        string  `json:"updatedAt"`
	RecurrenceRuleID *string `json:"recurrenceRuleId"`
	OccurrenceDate   *string `json:"occurrenceDate"`
	OccurrenceStatus *string `json:"occurrenceStatus"`
	// TotalFocusSeconds is the SUM of the task's closed focus segments (E-049).
	// Enriched only on Focus + GetTask; 0 on the project task-list, where a
	// per-row SUM would be pure cost for a value the list never renders.
	TotalFocusSeconds int64 `json:"totalFocusSeconds"`
	// SubtaskCount / OpenSubtaskCount are always populated, on every read.
	SubtaskCount     int `json:"subtaskCount"`
	OpenSubtaskCount int `json:"openSubtaskCount"`
}

// ListTasksResponse is the list response for tasks within a project.
type ListTasksResponse struct {
	Items      []TaskView `json:"items"`
	NextCursor string     `json:"nextCursor"`
}

// ListTasksFilter holds the parsed query params for the project task list.
// Nil pointer = filter not applied. SortField/SortOrder default to
// display_order asc when empty.
type ListTasksFilter struct {
	Status    *string
	Priority  *string
	Energy    *string
	Search    string
	SortField string
	SortOrder string
	// Cursor / Limit drive keyset pagination on (created_at, id) DESC.
	// The cursor key is independent of SortField/SortOrder so a drag-reorder
	// that mutates display_order cannot corrupt an in-flight cursor.
	Cursor string
	Limit  int
}

// CreateTaskRequest is the body for POST /projects/:projectId/tasks.
// Only title is required; everything else has a server-side default.
type CreateTaskRequest struct {
	Title            string  `json:"title"`
	Notes            *string `json:"notes"`
	Status           string  `json:"status"`
	Priority         string  `json:"priority"`
	Energy           string  `json:"energy"`
	RollsOver        *bool   `json:"rollsOver"`
	ScheduledFor     *string `json:"scheduledFor"`
	ScheduledTime    *string `json:"scheduledTime"`
	EstimatedMinutes *int    `json:"estimatedMinutes"`
	URL              *string `json:"url"`
}

// UpdateTaskRequest is the body for PATCH /tasks/:id — all fields optional.
type UpdateTaskRequest struct {
	Title     *string `json:"title"`
	Status    *string `json:"status"`
	Priority  *string `json:"priority"`
	Energy    *string `json:"energy"`
	RollsOver *bool   `json:"rollsOver"`
	// ProjectID reassigns the task to a different project the caller owns. A
	// task's project can never be cleared to null (SPEC hierarchy requires
	// every task to live in a project), so this is a plain pointer like
	// Status/Priority — not a tri-state optional.Field.
	ProjectID    *string                `json:"projectId"`
	Notes        optional.Field[string] `json:"notes"`
	ScheduledFor optional.Field[string] `json:"scheduledFor"`
	// ScheduledTime is tri-state: absent leaves the stored time alone, an
	// explicit null clears it. Clearing must stay reachable without a plan, so
	// the gate reads Set+Value rather than Set alone.
	ScheduledTime    optional.Field[string] `json:"scheduledTime"`
	EstimatedMinutes optional.Field[int]    `json:"estimatedMinutes"`
	URL              optional.Field[string] `json:"url"`
}

// SetStatusRequest is the body for PATCH /tasks/:id/status.
type SetStatusRequest struct {
	Status string `json:"status"`
}

// ScheduleRequest is the body for PATCH /tasks/:id/schedule.
// scheduledFor is the primary field of this endpoint, so null/absent both mean
// "unschedule"; a value sets the soft intention.
// scheduledTime rides the same convention: null/absent clears the time, which
// also keeps clearing a date from stranding a time on a now-unscheduled task.
type ScheduleRequest struct {
	ScheduledFor  *string `json:"scheduledFor"`
	ScheduledTime *string `json:"scheduledTime"`
	RollsOver     *bool   `json:"rollsOver"`
}

// ReorderOneRequest is the body for PATCH /tasks/:id/reorder.
type ReorderOneRequest struct {
	DisplayOrder int `json:"displayOrder"`
}

// TaskToView maps the domain model to its JSON response shape.
func TaskToView(t Task) TaskView {
	v := TaskView{
		ID:               t.ID,
		ProjectID:        t.ProjectID,
		Title:            t.Title,
		Notes:            t.Notes,
		Status:           t.Status,
		Priority:         t.Priority,
		Energy:           t.Energy,
		RollsOver:        t.RollsOver,
		ScheduledFor:     t.ScheduledFor,
		ScheduledTime:    t.ScheduledTime,
		EstimatedMinutes: t.EstimatedMinutes,
		URL:              t.URL,
		DisplayOrder:     t.DisplayOrder,
		CreatedAt:        t.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:        t.UpdatedAt.UTC().Format(time.RFC3339),
		RecurrenceRuleID: t.RecurrenceRuleID,
		OccurrenceDate:   t.OccurrenceDate,
		OccurrenceStatus: t.OccurrenceStatus,
		SubtaskCount:     t.SubtaskCount,
		OpenSubtaskCount: t.OpenSubtaskCount,
	}
	if t.CompletedAt != nil {
		s := t.CompletedAt.UTC().Format(time.RFC3339)
		v.CompletedAt = &s
	}
	return v
}
