package service

import (
	"context"

	"github.com/nicoflow/nicoflow-api/internal/model"
	"github.com/nicoflow/nicoflow-api/internal/repository"
)

type AreaService struct {
	areas repository.AreaRepo
	plans repository.UserPlanRepo
}

func NewAreaService(areas repository.AreaRepo, plans repository.UserPlanRepo) *AreaService {
	return &AreaService{areas: areas, plans: plans}
}

func (s *AreaService) Create(ctx context.Context, userID, name string) (*model.Area, error) {
	panic("not implemented")
}

func (s *AreaService) List(ctx context.Context, userID string) ([]model.Area, error) {
	return s.areas.List(ctx, userID)
}

func (s *AreaService) Update(ctx context.Context, id, userID, name string) (*model.Area, error) {
	panic("not implemented")
}

func (s *AreaService) Delete(ctx context.Context, id, userID string) error {
	panic("not implemented")
}
