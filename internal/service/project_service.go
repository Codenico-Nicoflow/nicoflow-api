package service

import (
	"context"

	"github.com/nicoflow/nicoflow-api/internal/model"
	"github.com/nicoflow/nicoflow-api/internal/repository"
)

type ProjectService struct {
	projects repository.ProjectRepo
	plans    repository.UserPlanRepo
}

func NewProjectService(projects repository.ProjectRepo, plans repository.UserPlanRepo) *ProjectService {
	return &ProjectService{projects: projects, plans: plans}
}

func (s *ProjectService) Create(ctx context.Context, userID, name string, areaID *string) (*model.Project, error) {
	panic("not implemented")
}

func (s *ProjectService) List(ctx context.Context, userID string) ([]model.Project, error) {
	return s.projects.List(ctx, userID)
}

func (s *ProjectService) Update(ctx context.Context, id, userID, name string, areaID *string) (*model.Project, error) {
	panic("not implemented")
}

func (s *ProjectService) Delete(ctx context.Context, id, userID string) error {
	panic("not implemented")
}
