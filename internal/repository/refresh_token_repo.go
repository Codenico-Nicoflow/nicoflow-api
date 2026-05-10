package repository

import (
	"context"

	"github.com/nicoflow/nicoflow-api/internal/model"
)

type RefreshTokenRepo interface {
	Create(ctx context.Context, t *model.RefreshToken) error
	FindByTokenHash(ctx context.Context, hash string) (*model.RefreshToken, error)
	DeleteByTokenHash(ctx context.Context, hash string) error
	DeleteAllByUserID(ctx context.Context, userID string) error
}
