package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"nicoflow-api/internal/model"
)

type AIUsageMonthlyRepo struct {
	db *pgxpool.Pool
}

func NewAIUsageMonthlyRepo(db *pgxpool.Pool) *AIUsageMonthlyRepo {
	return &AIUsageMonthlyRepo{db: db}
}

func (r *AIUsageMonthlyRepo) Upsert(ctx context.Context, u *model.AiUsageMonthly) error {
	_, err := r.db.Exec(ctx,
		`INSERT INTO ai_usage_monthly (id, user_id, month, request_count)
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT (user_id, month) DO UPDATE SET
		    request_count = ai_usage_monthly.request_count + 1`,
		u.ID, u.UserID, u.Month, u.RequestCount,
	)
	if err != nil {
		return fmt.Errorf("AIUsageMonthlyRepo.Upsert: %w", err)
	}
	return nil
}

func (r *AIUsageMonthlyRepo) FindByUserMonth(ctx context.Context, userID string, month time.Time) (*model.AiUsageMonthly, error) {
	u := &model.AiUsageMonthly{}
	err := r.db.QueryRow(ctx,
		`SELECT id, user_id, month, request_count
		 FROM ai_usage_monthly WHERE user_id = $1 AND month = $2`,
		userID, month,
	).Scan(&u.ID, &u.UserID, &u.Month, &u.RequestCount)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("AIUsageMonthlyRepo.FindByUserMonth: %w", err)
	}
	return u, nil
}
