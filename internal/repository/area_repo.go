package repository

import (
	"context"

	"github.com/nicoflow/nicoflow-api/internal/model"
)

type AreaRepo interface {
	Create(ctx context.Context, a *model.Area) error
	List(ctx context.Context, userID string) ([]model.Area, error)
	FindByID(ctx context.Context, id string) (*model.Area, error)
	Update(ctx context.Context, a *model.Area) error
	Delete(ctx context.Context, id string) error
}
