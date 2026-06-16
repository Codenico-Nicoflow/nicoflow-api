package project

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
)

const (
	freePlanProjectLimit = 5

	defaultStatus     = "active"
	defaultFolderIcon = "folder"
)

var allowedStatuses = map[string]bool{
	"active": true, "completed": true, "archived": true,
}

// Service defines the project business logic interface.
type Service interface {
	List(ctx context.Context, userID string, f ListProjectsFilter) (ListProjectsResponse, error)
	ListByArea(ctx context.Context, userID, areaID string, f ListProjectsFilter) (ListProjectsResponse, error)
	Get(ctx context.Context, userID, id string) (ProjectView, error)
	Create(ctx context.Context, userID, areaID, plan string, req CreateProjectRequest) (ProjectView, error)
	Update(ctx context.Context, userID, id string, req UpdateProjectRequest) (ProjectView, error)
	Delete(ctx context.Context, userID, id string) error
	Reorder(ctx context.Context, userID string, req ReorderRequest) (int, error)
}

type service struct {
	repo Repository
}

// NewService creates a new project Service.
func NewService(repo Repository) Service {
	return &service{repo: repo}
}

func (s *service) List(ctx context.Context, userID string, f ListProjectsFilter) (ListProjectsResponse, error) {
	projects, next, err := s.repo.List(ctx, userID, f)
	if err != nil {
		return ListProjectsResponse{}, err
	}
	return toListResponse(projects, next), nil
}

func (s *service) ListByArea(ctx context.Context, userID, areaID string, f ListProjectsFilter) (ListProjectsResponse, error) {
	projects, next, err := s.repo.ListByArea(ctx, userID, areaID, f)
	if err != nil {
		return ListProjectsResponse{}, err
	}
	return toListResponse(projects, next), nil
}

func (s *service) Get(ctx context.Context, userID, id string) (ProjectView, error) {
	p, err := s.repo.GetByID(ctx, userID, id)
	if err != nil {
		return ProjectView{}, err
	}
	return ProjectToView(*p), nil
}

func (s *service) Create(ctx context.Context, userID, areaID, plan string, req CreateProjectRequest) (ProjectView, error) {
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		return ProjectView{}, apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidInput, "name is required")
	}
	if len(req.Name) > 255 {
		return ProjectView{}, apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidInput, "name must be 255 characters or fewer")
	}

	if req.Status == "" {
		req.Status = defaultStatus
	} else if !allowedStatuses[req.Status] {
		return ProjectView{}, apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidStatus, "status must be one of: active, completed, archived")
	}

	if req.FolderIcon == "" {
		req.FolderIcon = defaultFolderIcon
	} else if !AllowedIcons[req.FolderIcon] {
		return ProjectView{}, apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidInput, "folderIcon is not valid")
	}

	if req.Description != nil && len(*req.Description) > 2000 {
		return ProjectView{}, apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidInput, "description must be 2000 characters or fewer")
	}

	if plan == "free" {
		count, err := s.repo.CountByUser(ctx, userID)
		if err != nil {
			return ProjectView{}, fmt.Errorf("project.Create count: %w", err)
		}
		if count >= freePlanProjectLimit {
			return ProjectView{}, apperror.New(http.StatusForbidden, apperror.ErrPlanLimitExceeded, "free plan allows up to 5 projects")
		}
	}

	var areaIDPtr *string
	if areaID != "" {
		areaIDPtr = &areaID
	}

	p, err := s.repo.Create(ctx, Project{
		ID:          uuid.New().String(),
		UserID:      userID,
		AreaID:      areaIDPtr,
		Name:        req.Name,
		Status:      req.Status,
		FolderIcon:  req.FolderIcon,
		DueDate:     req.DueDate,
		IsFavorite:  req.IsFavorite,
		Description: req.Description,
	})
	if err != nil {
		return ProjectView{}, err
	}
	return ProjectToView(p), nil
}

func (s *service) Update(ctx context.Context, userID, id string, req UpdateProjectRequest) (ProjectView, error) {
	if req.Name != nil {
		*req.Name = strings.TrimSpace(*req.Name)
		if *req.Name == "" {
			return ProjectView{}, apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidInput, "name cannot be empty")
		}
		if len(*req.Name) > 255 {
			return ProjectView{}, apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidInput, "name must be 255 characters or fewer")
		}
	}
	if req.Status != nil && !allowedStatuses[*req.Status] {
		return ProjectView{}, apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidStatus, "status must be one of: active, completed, archived")
	}
	if req.FolderIcon != nil && !AllowedIcons[*req.FolderIcon] {
		return ProjectView{}, apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidInput, "folderIcon is not valid")
	}
	if req.Description != nil && len(*req.Description) > 2000 {
		return ProjectView{}, apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidInput, "description must be 2000 characters or fewer")
	}

	p, err := s.repo.Update(ctx, userID, id, req)
	if err != nil {
		return ProjectView{}, err
	}
	return ProjectToView(p), nil
}

func (s *service) Delete(ctx context.Context, userID, id string) error {
	return s.repo.Delete(ctx, userID, id)
}

func (s *service) Reorder(ctx context.Context, userID string, req ReorderRequest) (int, error) {
	if len(req.Items) == 0 {
		return 0, apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidInput, "items must not be empty")
	}
	return s.repo.Reorder(ctx, userID, req.Items)
}

func toListResponse(projects []Project, next string) ListProjectsResponse {
	items := make([]ProjectView, len(projects))
	for i, p := range projects {
		items[i] = ProjectToView(p)
	}
	return ListProjectsResponse{Items: items, NextCursor: next}
}
