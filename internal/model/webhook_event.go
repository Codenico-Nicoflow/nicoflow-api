package model

import (
	"encoding/json"
	"time"
)

type WebhookEvent struct {
	ID                  string          `json:"id"`
	LemonSqueezyEventID string          `json:"lemon_squeezy_event_id"`
	EventName           string          `json:"event_name"`
	Payload             json.RawMessage `json:"payload"`
	ProcessedAt         time.Time       `json:"processed_at"`
	Error               *string         `json:"error,omitempty"`
}
