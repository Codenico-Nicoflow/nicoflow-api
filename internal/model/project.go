package model

import "time"

type Project struct {
	ID           string    `json:"id"`
	UserID       string    `json:"user_id"`
	AreaID       *string   `json:"aread_id,omitempty"`
	Name         string    `json:"name"`
	Status       string    `json:"status"`
	DisplayOrder int       `json:"display_order"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}
