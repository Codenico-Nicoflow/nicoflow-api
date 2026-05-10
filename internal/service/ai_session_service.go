package service

import (
	"context"

	"github.com/nicoflow/nicoflow-api/internal/model"
	"github.com/nicoflow/nicoflow-api/internal/repository"
)

type AISessionService struct {
	sessions repository.AISessionRepo
	plans    repository.UserPlanRepo
}

func NewAISessionService(sessions repository.AISessionRepo, plans repository.UserPlanRepo) *AISessionService {
	return &AISessionService{sessions: sessions, plans: plans}
}

func (s *AISessionService) Create(ctx context.Context, userID string) (*model.AISession, error) {
	panic("not implemented")
}

func (s *AISessionService) List(ctx context.Context, userID string) ([]model.AISession, error) {
	return s.sessions.List(ctx, userID)
}

func (s *AISessionService) Delete(ctx context.Context, id, userID string) error {
	panic("not implemented")
}
