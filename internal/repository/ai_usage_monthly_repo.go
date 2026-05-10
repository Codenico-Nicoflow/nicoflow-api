package repository

import (
	"context"
	"time"

	"github.com/nicoflow/nicoflow-api/internal/model"
)

type AIUsageMonthlyRepo interface {
	Upsert(ctx context.Context, u *model.AiUsageMonthly) error
	FindByUserMonth(ctx context.Context, userID string, month time.Time) (*model.AiUsageMonthly, error)
}
