package service

import (
	"context"

	"nicoflow-api/internal/model"
	"nicoflow-api/internal/repository"
)

type UserPlanService struct {
	plans repository.UserPlanRepo
}

func NewUserPlanService(plans repository.UserPlanRepo) *UserPlanService {
	return &UserPlanService{plans: plans}
}

func (s *UserPlanService) Get(ctx context.Context, userID string) (*model.UserPlan, error) {
	return s.plans.FindByUserID(ctx, userID)
}
