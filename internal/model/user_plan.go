package model

import "time"

type UserPlan struct {
	ID                         string     `json:"id"`
	UserID                     string     `json:"user_id"`
	Plan                       string     `json:"plan"`   // "free" | "pro"
	Status                     string     `json:"status"` // "active" | "cancelled" | "expired" | "paused"
	LemonSqueezySubscriptionID *string    `json:"lemon_squeezy_subscription_id,omitempty"`
	LemonSqueezyCustomerID     *string    `json:"lemon_squeezy_customer_id,omitempty"`
	CurrentPeriodStart         *time.Time `json:"current_period_start,omitempty"`
	CurrentPeriodEnd           *time.Time `json:"current_period_end,omitempty"`
	CreatedAt                  time.Time  `json:"created_at"`
	UpdatedAt                  time.Time  `json:"updated_at"`
}
