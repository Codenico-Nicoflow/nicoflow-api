package service

import (
	"context"

	"nicoflow-api/internal/repository"
)

type TimeSpreadService struct {
	tasks repository.TaskRepo
}

func NewTimeSpreadService(tasks repository.TaskRepo) *TimeSpreadService {
	return &TimeSpreadService{tasks: tasks}
}

func (s *TimeSpreadService) Get(ctx context.Context, userID string) (interface{}, error) {
	panic("not implemented")
}
