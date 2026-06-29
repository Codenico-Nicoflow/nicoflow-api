package task

import "time"

// Subtask is the internal domain model for a subtask.
type Subtask struct {
	ID        string
	TaskID    string
	Title     string
	Done      bool
	Position  int
	CreatedAt time.Time
	UpdatedAt time.Time
}

// SubtaskView is the JSON response shape (ISubtask).
type SubtaskView struct {
	ID        string `json:"id"`
	TaskID    string `json:"taskId"`
	Title     string `json:"title"`
	Done      bool   `json:"done"`
	Position  int    `json:"position"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

// ListSubtasksResponse is the list response.
type ListSubtasksResponse struct {
	Items []SubtaskView `json:"items"`
}

// CreateSubtaskRequest is the body for POST /tasks/:taskId/subtasks.
type CreateSubtaskRequest struct {
	Title    string `json:"title"`
	Position *int   `json:"position"`
}

// UpdateSubtaskRequest is the body for PATCH /tasks/:taskId/subtasks/:id.
type UpdateSubtaskRequest struct {
	Title    *string `json:"title"`
	Done     *bool   `json:"done"`
	Position *int    `json:"position"`
}

// SubtaskToView maps the domain model to its JSON shape.
func SubtaskToView(s Subtask) SubtaskView {
	return SubtaskView{
		ID:        s.ID,
		TaskID:    s.TaskID,
		Title:     s.Title,
		Done:      s.Done,
		Position:  s.Position,
		CreatedAt: s.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt: s.UpdatedAt.UTC().Format(time.RFC3339),
	}
}
