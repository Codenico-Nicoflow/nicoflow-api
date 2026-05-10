package service

import (
	"context"

	"github.com/nicoflow/nicoflow-api/internal/model"
	"github.com/nicoflow/nicoflow-api/internal/repository"
)

type SubtaskService struct {
	subtasks repository.SubtaskRepo
}

func NewSubtaskService(subtasks repository.SubtaskRepo) *SubtaskService {
	return &SubtaskService{subtasks: subtasks}
}

func (s *SubtaskService) Create(ctx context.Context, taskID, title string) (*model.Subtask, error) {
	panic("not implemented")
}

func (s *SubtaskService) ListByTask(ctx context.Context, taskID string) ([]model.Subtask, error) {
	return s.subtasks.ListByTask(ctx, taskID)
}

func (s *SubtaskService) Update(ctx context.Context, id, taskID string, done bool, title string) (*model.Subtask, error) {
	panic("not implemented")
}

func (s *SubtaskService) Delete(ctx context.Context, id, taskID string) error {
	panic("not implemented")
}
