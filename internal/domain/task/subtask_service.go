package task

import (
	"context"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
)

const maxSubtaskTitleLen = 255

// SubtaskService is the business logic for subtasks. Every call verifies the
// parent task belongs to the user.
type SubtaskService interface {
	List(ctx context.Context, userID, taskID string) (ListSubtasksResponse, error)
	Create(ctx context.Context, userID, taskID string, req CreateSubtaskRequest) (SubtaskView, error)
	Update(ctx context.Context, userID, taskID, id string, req UpdateSubtaskRequest) (SubtaskView, error)
	Delete(ctx context.Context, userID, taskID, id string) error
}

type subtaskService struct{ repo SubtaskRepository }

// NewSubtaskService creates a new SubtaskService.
func NewSubtaskService(repo SubtaskRepository) SubtaskService { return &subtaskService{repo: repo} }

func (s *subtaskService) requireTask(ctx context.Context, userID, taskID string) error {
	owned, err := s.repo.TaskOwned(ctx, userID, taskID)
	if err != nil {
		return err
	}
	if !owned {
		return apperror.New(http.StatusNotFound, apperror.ErrResourceNotFound, "task not found")
	}
	return nil
}

func (s *subtaskService) List(ctx context.Context, userID, taskID string) (ListSubtasksResponse, error) {
	if err := s.requireTask(ctx, userID, taskID); err != nil {
		return ListSubtasksResponse{}, err
	}
	subtasks, err := s.repo.ListByTask(ctx, taskID)
	if err != nil {
		return ListSubtasksResponse{}, err
	}
	items := make([]SubtaskView, len(subtasks))
	for i, st := range subtasks {
		items[i] = SubtaskToView(st)
	}
	return ListSubtasksResponse{Items: items}, nil
}

func (s *subtaskService) Create(ctx context.Context, userID, taskID string, req CreateSubtaskRequest) (SubtaskView, error) {
	req.Title = strings.TrimSpace(req.Title)
	if req.Title == "" {
		return SubtaskView{}, apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidInput, "title is required")
	}
	if len(req.Title) > maxSubtaskTitleLen {
		return SubtaskView{}, apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidInput, "title must be 255 characters or fewer")
	}
	if err := s.requireTask(ctx, userID, taskID); err != nil {
		return SubtaskView{}, err
	}

	position := 0
	if req.Position != nil {
		position = *req.Position
	} else {
		next, err := s.repo.NextPosition(ctx, taskID)
		if err != nil {
			return SubtaskView{}, err
		}
		position = next
	}

	created, err := s.repo.Create(ctx, Subtask{
		ID:       uuid.New().String(),
		TaskID:   taskID,
		Title:    req.Title,
		Position: position,
	})
	if err != nil {
		return SubtaskView{}, err
	}
	return SubtaskToView(created), nil
}

func (s *subtaskService) Update(ctx context.Context, userID, taskID, id string, req UpdateSubtaskRequest) (SubtaskView, error) {
	if req.Title != nil {
		*req.Title = strings.TrimSpace(*req.Title)
		if *req.Title == "" {
			return SubtaskView{}, apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidInput, "title cannot be empty")
		}
		if len(*req.Title) > maxSubtaskTitleLen {
			return SubtaskView{}, apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidInput, "title must be 255 characters or fewer")
		}
	}
	updated, err := s.repo.Update(ctx, userID, taskID, id, req)
	if err != nil {
		return SubtaskView{}, err
	}
	return SubtaskToView(updated), nil
}

func (s *subtaskService) Delete(ctx context.Context, userID, taskID, id string) error {
	return s.repo.Delete(ctx, userID, taskID, id)
}
