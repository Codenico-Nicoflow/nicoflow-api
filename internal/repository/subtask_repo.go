package repository

import (
	"context"

	"nicoflow-api/internal/model"
)

type SubtaskRepo interface {
	Create(ctx context.Context, s *model.Subtask) error
	ListByTask(ctx context.Context, taskID string) ([]model.Subtask, error)
	FindByID(ctx context.Context, id string) (*model.Subtask, error)
	Update(ctx context.Context, s *model.Subtask) error
	Delete(ctx context.Context, id string) error
}
