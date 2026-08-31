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
	repo        Repository
	broadcaster Broadcaster // nil disables emission
	notif       notifier    // nil disables notification emission
}

// NewService creates a new project Service. broadcaster and notif may be nil
// (real-time / notification emission disabled respectively); pass the ws
// adapter and notification.Service to light both up.
func NewService(repo Repository, broadcaster Broadcaster, notif notifier) Service {
	return &service{repo: repo, broadcaster: broadcaster, notif: notif}
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

	if areaID == "" {
		return ProjectView{}, apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidInput, "areaId is required")
	}

	p, err := s.repo.Create(ctx, Project{
		ID:          uuid.New().String(),
		UserID:      userID,
		AreaID:      areaID,
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
	view := ProjectToView(p)
	s.emit(userID, Event{Type: EventCreated, Payload: view})
	return view, nil
}

// validateUpdateRequest checks field-level constraints on an update request,
// normalising Name in place (trim). Split out of Update to keep that
// function's cyclomatic complexity down.
func validateUpdateRequest(req UpdateProjectRequest) error {
	if req.Name != nil {
		*req.Name = strings.TrimSpace(*req.Name)
		if *req.Name == "" {
			return apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidInput, "name cannot be empty")
		}
		if len(*req.Name) > 255 {
			return apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidInput, "name must be 255 characters or fewer")
		}
	}
	if req.AreaID != nil && strings.TrimSpace(*req.AreaID) == "" {
		return apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidInput, "areaId cannot be empty")
	}
	if req.Status != nil && !allowedStatuses[*req.Status] {
		return apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidStatus, "status must be one of: active, completed, archived")
	}
	if req.FolderIcon != nil && !AllowedIcons[*req.FolderIcon] {
		return apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidInput, "folderIcon is not valid")
	}
	if desc, ok := req.Description.Get(); ok && len(desc) > 2000 {
		return apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidInput, "description must be 2000 characters or fewer")
	}
	return nil
}

func (s *service) Update(ctx context.Context, userID, id string, req UpdateProjectRequest) (ProjectView, error) {
	if err := validateUpdateRequest(req); err != nil {
		return ProjectView{}, err
	}

	// Status transitions need the prior value to detect the edge into
	// "completed" — fetched only when the request actually touches status.
	var prevStatus string
	if req.Status != nil {
		current, err := s.repo.GetByID(ctx, userID, id)
		if err != nil {
			return ProjectView{}, err
		}
		prevStatus = current.Status
	}

	p, err := s.repo.Update(ctx, userID, id, req)
	if err != nil {
		return ProjectView{}, err
	}
	view := ProjectToView(p)
	s.emit(userID, Event{Type: EventUpdated, Payload: view})
	if req.Status != nil {
		s.emitProjectCompletedIfTransitioned(ctx, prevStatus, p)
	}
	return view, nil
}

func (s *service) Delete(ctx context.Context, userID, id string) error {
	if err := s.repo.Delete(ctx, userID, id); err != nil {
		return err
	}
	s.emit(userID, Event{Type: EventDeleted, Payload: Ref{ID: id}})
	return nil
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
