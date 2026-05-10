package repository

import (
	"context"

	"github.com/nicoflow/nicoflow-api/internal/model"
)

type AISessionRepo interface {
	Create(ctx context.Context, s *model.AISession) error
	List(ctx context.Context, userID string) ([]model.AISession, error)
	FindByID(ctx context.Context, id string) (*model.AISession, error)
	Delete(ctx context.Context, id string) error
}
