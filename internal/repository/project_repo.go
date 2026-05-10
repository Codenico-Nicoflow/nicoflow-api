package repository

import (
	"context"

	"github.com/nicoflow/nicoflow-api/internal/model"
)

type ProjectRepo interface {
	Create(ctx context.Context, p *model.Project) error
	List(ctx context.Context, userID string) ([]model.Project, error)
	FindByID(ctx context.Context, id string) (*model.Project, error)
	Update(ctx context.Context, p *model.Project) error
	Delete(ctx context.Context, id string) error
}
