// Package task contains the task and subtask domain.
package task

import "time"

// Task is the internal domain model for a task.
type Task struct {
	ID               string
	UserID           string
	ProjectID        string
	Title            string
	Notes            *string
	Status           string
	Priority         string
	Energy           string
	RollsOver        bool
	DueDate          *time.Time
	ScheduledFor     *string
	EstimatedMinutes *int
	URL              *string
	DisplayOrder     int
	CompletedAt      *time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
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
	DueDate          *string `json:"dueDate"`
	ScheduledFor     *string `json:"scheduledFor"`
	EstimatedMinutes *int    `json:"estimatedMinutes"`
	URL              *string `json:"url"`
	DisplayOrder     int     `json:"displayOrder"`
	CompletedAt      *string `json:"completedAt"`
	CreatedAt        string  `json:"createdAt"`
	UpdatedAt        string  `json:"updatedAt"`
}

// ListTasksResponse is the list response for tasks within a project.
type ListTasksResponse struct {
	Items []TaskView `json:"items"`
}

// CreateTaskRequest is the body for POST /projects/:projectId/tasks.
// Only title is required; everything else has a server-side default.
type CreateTaskRequest struct {
	Title            string     `json:"title"`
	Notes            *string    `json:"notes"`
	Status           string     `json:"status"`
	Priority         string     `json:"priority"`
	Energy           string     `json:"energy"`
	RollsOver        *bool      `json:"rollsOver"`
	DueDate          *time.Time `json:"dueDate"`
	ScheduledFor     *string    `json:"scheduledFor"`
	EstimatedMinutes *int       `json:"estimatedMinutes"`
	URL              *string    `json:"url"`
}

// UpdateTaskRequest is the body for PATCH /tasks/:id — all fields optional.
type UpdateTaskRequest struct {
	Title            *string    `json:"title"`
	Notes            *string    `json:"notes"`
	Status           *string    `json:"status"`
	Priority         *string    `json:"priority"`
	Energy           *string    `json:"energy"`
	RollsOver        *bool      `json:"rollsOver"`
	DueDate          *time.Time `json:"dueDate"`
	ScheduledFor     *string    `json:"scheduledFor"`
	EstimatedMinutes *int       `json:"estimatedMinutes"`
	URL              *string    `json:"url"`
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
		EstimatedMinutes: t.EstimatedMinutes,
		URL:              t.URL,
		DisplayOrder:     t.DisplayOrder,
		CreatedAt:        t.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:        t.UpdatedAt.UTC().Format(time.RFC3339),
	}
	if t.DueDate != nil {
		s := t.DueDate.UTC().Format(time.RFC3339)
		v.DueDate = &s
	}
	if t.CompletedAt != nil {
		s := t.CompletedAt.UTC().Format(time.RFC3339)
		v.CompletedAt = &s
	}
	return v
}
