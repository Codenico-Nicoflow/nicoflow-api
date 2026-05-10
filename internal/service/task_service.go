package service

import (
	"context"

	"github.com/nicoflow/nicoflow-api/internal/model"
	"github.com/nicoflow/nicoflow-api/internal/repository"
)

type TaskService struct {
	tasks repository.TaskRepo
}

func NewTaskService(tasks repository.TaskRepo) *TaskService {
	return &TaskService{tasks: tasks}
}

func (s *TaskService) Create(ctx context.Context, userID, title string, projectID *string, dueDate *string, scheduledFor *string) (*model.Task, error) {
	panic("not implemented")
}

func (s *TaskService) List(ctx context.Context, userID string) ([]model.Task, error) {
	return s.tasks.List(ctx, userID)
}

func (s *TaskService) Update(ctx context.Context, id, userID string, updates *model.Task) (*model.Task, error) {
	panic("not implemented")
}

func (s *TaskService) Delete(ctx context.Context, id, userID string) error {
	panic("not implemented")
}
