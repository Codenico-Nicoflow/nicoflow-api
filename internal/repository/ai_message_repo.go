package repository

import (
	"context"

	"github.com/nicoflow/nicoflow-api/internal/model"
)

type AIMessageRepo interface {
	Create(ctx context.Context, m *model.AIMessage) error
	ListBySession(ctx context.Context, sessionID string) ([]model.AIMessage, error)
}
