package repository

import (
	"context"

	"nicoflow-api/internal/model"
)

type UserPlanRepo interface {
	FindByUserID(ctx context.Context, userID string) (*model.UserPlan, error)
	Upsert(ctx context.Context, p *model.UserPlan) error
}
