package model

import "time"

type Task struct {
	ID           string     `json:"id"`
	UserID       string     `json:"user_id"`
	ProjectID    *string    `json:"project_id,omitempty"`
	Title        string     `json:"title"`
	Notes        string     `json:"notes"`
	DueDate      *string    `json:"due_date,omitempty"`    // DATE as "YYYY-MM-DD"
	ScheduledFor *string    `json:"scheduled_for,omitempty"` // "today" | "tomorrow" | "this_week"
	Status       string     `json:"status"`                // "inbox" | "active" | "done" | "cancelled"
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}
