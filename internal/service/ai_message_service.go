package service

import (
	"context"

	"github.com/nicoflow/nicoflow-api/internal/model"
	"github.com/nicoflow/nicoflow-api/internal/repository"
)

type AIMessageService struct {
	messages repository.AIMessageRepo
	sessions repository.AISessionRepo
	aiUsage  repository.AIUsageMonthlyRepo
	plans    repository.UserPlanRepo
}

func NewAIMessageService(
	messages repository.AIMessageRepo,
	sessions repository.AISessionRepo,
	aiUsage repository.AIUsageMonthlyRepo,
	plans repository.UserPlanRepo,
) *AIMessageService {
	return &AIMessageService{messages: messages, sessions: sessions, aiUsage: aiUsage, plans: plans}
}

func (s *AIMessageService) Send(ctx context.Context, userID, sessionID, content string) (*model.AIMessage, error) {
	panic("not implemented")
}

func (s *AIMessageService) ListBySession(ctx context.Context, userID, sessionID string) ([]model.AIMessage, error) {
	panic("not implemented")
}
