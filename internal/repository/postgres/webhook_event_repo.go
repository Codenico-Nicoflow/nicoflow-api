package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"nicoflow-api/internal/model"
)

type WebhookEventRepo struct {
	db *pgxpool.Pool
}

func NewWebhookEventRepo(db *pgxpool.Pool) *WebhookEventRepo {
	return &WebhookEventRepo{db: db}
}

func (r *WebhookEventRepo) EventExists(ctx context.Context, lemonSqueezyEventID string) (bool, error) {
	var exists bool
	err := r.db.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM webhook_events WHERE lemon_squeezy_event_id = $1)`,
		lemonSqueezyEventID,
	).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("WebhookEventRepo.EventExists: %w", err)
	}
	return exists, nil
}

func (r *WebhookEventRepo) Save(ctx context.Context, e *model.WebhookEvent) error {
	_, err := r.db.Exec(ctx,
		`INSERT INTO webhook_events (id, lemon_squeezy_event_id, event_name, payload, processed_at, error)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		e.ID, e.LemonSqueezyEventID, e.EventName, e.Payload, e.ProcessedAt, e.Error,
	)
	if err != nil {
		return fmt.Errorf("WebhookEventRepo.Save: %w", err)
	}
	return nil
}
