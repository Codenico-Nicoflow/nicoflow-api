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

// The wire enums. swaggo derives the OpenAPI `enum` from the ordered consts of a
// named string type, which is what lets the generated TypeScript be a literal
// union instead of a bare `string`. Values must match the DB CHECK constraints.
type (
	// TaskStatus mirrors the tasks_status_check constraint (migration 025).
	TaskStatus string
	// TaskPriority is the user-set urgency of a task.
	TaskPriority string
	// TaskEnergy is the focus cost of a task (SPEC §3.4).
	TaskEnergy string
	// TaskOccurrenceStatus is set only on materialized recurring occurrences and
	// is nil on an ordinary or still-live one (E-050).
	TaskOccurrenceStatus string
)

const (
	TaskStatusActive    TaskStatus = "active"
	TaskStatusDone      TaskStatus = "done"
	TaskStatusCancelled TaskStatus = "cancelled"
)

const (
	TaskPriorityLow    TaskPriority = "low"
	TaskPriorityMedium TaskPriority = "medium"
	TaskPriorityHigh   TaskPriority = "high"
)

const (
	TaskEnergyLow    TaskEnergy = "low"
	TaskEnergyMedium TaskEnergy = "medium"
	TaskEnergyDeep   TaskEnergy = "deep"
)

const (
	TaskOccurrenceStatusMissed    TaskOccurrenceStatus = "missed"
	TaskOccurrenceStatusCancelled TaskOccurrenceStatus = "cancelled"
	TaskOccurrenceStatusSkipped   TaskOccurrenceStatus = "skipped"
)

// TaskView is the JSON response shape (ITask) for a single task.
type TaskView struct {
	ID               string                `json:"id" validate:"required"`
	ProjectID        string                `json:"projectId" validate:"required"`
	Title            string                `json:"title" validate:"required"`
	Notes            *string               `json:"notes" extensions:"x-nullable"`
	Status           TaskStatus            `json:"status" validate:"required"`
	Priority         TaskPriority          `json:"priority" validate:"required"`
	Energy           TaskEnergy            `json:"energy" validate:"required"`
	RollsOver        bool                  `json:"rollsOver" validate:"required"`
	ScheduledFor     *string               `json:"scheduledFor" format:"date" extensions:"x-nullable"`
	ScheduledTime    *string               `json:"scheduledTime" extensions:"x-nullable"`
	EstimatedMinutes *int                  `json:"estimatedMinutes" extensions:"x-nullable"`
	URL              *string               `json:"url" extensions:"x-nullable"`
	DisplayOrder     int                   `json:"displayOrder" validate:"required"`
	CompletedAt      *string               `json:"completedAt" format:"date-time" extensions:"x-nullable"`
	CreatedAt        string                `json:"createdAt" format:"date-time" validate:"required"`
	UpdatedAt        string                `json:"updatedAt" format:"date-time" validate:"required"`
	RecurrenceRuleID *string               `json:"recurrenceRuleId" extensions:"x-nullable"`
	OccurrenceDate   *string               `json:"occurrenceDate" format:"date" extensions:"x-nullable"`
	OccurrenceStatus *TaskOccurrenceStatus `json:"occurrenceStatus" extensions:"x-nullable"`
	// TotalFocusSeconds is the SUM of the task's closed focus segments (E-049).
	// Enriched only on Focus + GetTask; 0 on the project task-list, where a
	// per-row SUM would be pure cost for a value the list never renders.
	TotalFocusSeconds int64 `json:"totalFocusSeconds" validate:"required"`
	// SubtaskCount / OpenSubtaskCount are always populated, on every read.
	SubtaskCount     int `json:"subtaskCount" validate:"required"`
	OpenSubtaskCount int `json:"openSubtaskCount" validate:"required"`
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

// occurrenceStatusPtr converts the domain model's untyped pointer to the wire
// enum, preserving nil. The domain model stays plain strings so SQL scanning is
// unaffected; only the view carries the enum type swaggo reads.
func occurrenceStatusPtr(s *string) *TaskOccurrenceStatus {
	if s == nil {
		return nil
	}
	v := TaskOccurrenceStatus(*s)
	return &v
}

// TaskToView maps the domain model to its JSON response shape.
func TaskToView(t Task) TaskView {
	v := TaskView{
		ID:               t.ID,
		ProjectID:        t.ProjectID,
		Title:            t.Title,
		Notes:            t.Notes,
		Status:           TaskStatus(t.Status),
		Priority:         TaskPriority(t.Priority),
		Energy:           TaskEnergy(t.Energy),
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
		OccurrenceStatus: occurrenceStatusPtr(t.OccurrenceStatus),
		SubtaskCount:     t.SubtaskCount,
		OpenSubtaskCount: t.OpenSubtaskCount,
	}
	if t.CompletedAt != nil {
		s := t.CompletedAt.UTC().Format(time.RFC3339)
		v.CompletedAt = &s
	}
	return v
}
