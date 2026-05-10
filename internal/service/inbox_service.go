package service

import (
	"context"

	"github.com/nicoflow/nicoflow-api/internal/model"
	"github.com/nicoflow/nicoflow-api/internal/repository"
)

type InboxService struct {
	tasks repository.TaskRepo
}

func NewInboxService(tasks repository.TaskRepo) *InboxService {
	return &InboxService{tasks: tasks}
}

func (s *InboxService) Capture(ctx context.Context, userID, title string) (*model.Task, error) {
	panic("not implemented")
}

func (s *InboxService) List(ctx context.Context, userID string) ([]model.Task, error) {
	panic("not implemented")
}
