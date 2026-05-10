package repository

import (
	"context"

	"github.com/nicoflow/nicoflow-api/internal/model"
)

type TaskRepo interface {
	Create(ctx context.Context, t *model.Task) error
	List(ctx context.Context, userID string) ([]model.Task, error)
	FindByID(ctx context.Context, id string) (*model.Task, error)
	Update(ctx context.Context, t *model.Task) error
	Delete(ctx context.Context, id string) error
}
