package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"nicoflow-api/internal/model"
)

type UserPlanRepo struct {
	db *pgxpool.Pool
}

func NewUserPlanRepo(db *pgxpool.Pool) *UserPlanRepo {
	return &UserPlanRepo{db: db}
}

func (r *UserPlanRepo) FindByUserID(ctx context.Context, userID string) (*model.UserPlan, error) {
	p := &model.UserPlan{}
	err := r.db.QueryRow(ctx,
		`SELECT id, user_id, plan, status, lemon_squeezy_subscription_id, lemon_squeezy_customer_id,
		        current_period_start, current_period_end, created_at, updated_at
		 FROM user_plans WHERE user_id = $1`,
		userID,
	).Scan(
		&p.ID, &p.UserID, &p.Plan, &p.Status,
		&p.LemonSqueezySubscriptionID, &p.LemonSqueezyCustomerID,
		&p.CurrentPeriodStart, &p.CurrentPeriodEnd,
		&p.CreatedAt, &p.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("UserPlanRepo.FindByUserID: %w", err)
	}
	return p, nil
}

func (r *UserPlanRepo) Upsert(ctx context.Context, p *model.UserPlan) error {
	_, err := r.db.Exec(ctx,
		`INSERT INTO user_plans
		    (id, user_id, plan, status, lemon_squeezy_subscription_id, lemon_squeezy_customer_id,
		     current_period_start, current_period_end, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		 ON CONFLICT (user_id) DO UPDATE SET
		    plan = EXCLUDED.plan,
		    status = EXCLUDED.status,
		    lemon_squeezy_subscription_id = EXCLUDED.lemon_squeezy_subscription_id,
		    lemon_squeezy_customer_id = EXCLUDED.lemon_squeezy_customer_id,
		    current_period_start = EXCLUDED.current_period_start,
		    current_period_end = EXCLUDED.current_period_end,
		    updated_at = EXCLUDED.updated_at`,
		p.ID, p.UserID, p.Plan, p.Status,
		p.LemonSqueezySubscriptionID, p.LemonSqueezyCustomerID,
		p.CurrentPeriodStart, p.CurrentPeriodEnd,
		p.CreatedAt, p.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("UserPlanRepo.Upsert: %w", err)
	}
	return nil
}
