package model

import "time"

type AiUsageMonthly struct {
	ID           string    `json:"id"`
	UserID       string    `json:"user_id"`
	Month        time.Time `json:"month"` // DATE — first day of the month
	RequestCount int       `json:"request_count"`
}
