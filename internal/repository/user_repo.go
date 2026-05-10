package repository

import (
	"context"

	"github.com/nicoflow/nicoflow-api/internal/model"
)

type UserRepo interface {
	Create(ctx context.Context, u *model.User) error
	FindByEmail(ctx context.Context, email string) (*model.User, error)
	FindByID(ctx context.Context, id string) (*model.User, error)
	SoftDelete(ctx context.Context, id string) error
}
