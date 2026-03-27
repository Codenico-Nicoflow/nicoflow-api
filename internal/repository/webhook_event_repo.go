package repository

import (
	"context"

	"nicoflow-api/internal/model"
)

type WebhookEventRepo interface {
	EventExists(ctx context.Context, lemonSqueezyEventID string) (bool, error)
	Save(ctx context.Context, e *model.WebhookEvent) error
}
