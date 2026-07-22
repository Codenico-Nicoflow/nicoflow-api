package area

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/google/uuid"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
	"github.com/nicoflow/nicoflow-api/internal/domain/project"
)

const (
	freePlanAreaLimit = 3

	defaultColor = "#3B82F6"
	defaultIcon  = "folder"
)

// colorRE validates 6-digit hex colors. Icons are validated against the shared
// project.AllowedIcons set so areas and projects accept the same icons.
var colorRE = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)

// Service defines the area business logic interface.
type Service interface {
	List(ctx context.Context, userID string, f ListAreasFilter) (ListAreasResponse, error)
	ListWithProjects(ctx context.Context, userID string) ([]AreaWithProjectsView, error)
	Get(ctx context.Context, userID, id string) (AreaView, error)
	Create(ctx context.Context, userID, plan string, req CreateAreaRequest) (AreaView, error)
	Update(ctx context.Context, userID, id string, req UpdateAreaRequest) (AreaView, error)
	Delete(ctx context.Context, userID, id string) error
	Reorder(ctx context.Context, userID string, req ReorderRequest) (int, error)
}

type service struct {
	repo        Repository
	broadcaster Broadcaster // nil disables emission
}

// NewService creates a new area Service. broadcaster may be nil (real-time
// emission disabled); pass the ws adapter to light up live updates.
func NewService(repo Repository, broadcaster Broadcaster) Service {
	return &service{repo: repo, broadcaster: broadcaster}
}

func (s *service) List(ctx context.Context, userID string, f ListAreasFilter) (ListAreasResponse, error) {
	areas, next, err := s.repo.List(ctx, userID, f)
	if err != nil {
		return ListAreasResponse{}, err
	}
	items := make([]AreaView, len(areas))
	for i, a := range areas {
		items[i] = areaToView(a)
	}
	return ListAreasResponse{Items: items, NextCursor: next}, nil
}

func (s *service) ListWithProjects(ctx context.Context, userID string) ([]AreaWithProjectsView, error) {
	awps, err := s.repo.ListWithProjects(ctx, userID)
	if err != nil {
		return nil, err
	}
	views := make([]AreaWithProjectsView, len(awps))
	for i, awp := range awps {
		pViews := make([]project.ProjectView, len(awp.Projects))
		for j, p := range awp.Projects {
			pViews[j] = project.ProjectToView(p)
		}
		views[i] = AreaWithProjectsView{
			AreaView: areaToView(awp.Area),
			Projects: pViews,
		}
	}
	return views, nil
}

func (s *service) Get(ctx context.Context, userID, id string) (AreaView, error) {
	a, err := s.repo.GetByID(ctx, userID, id)
	if err != nil {
		return AreaView{}, err
	}
	return areaToView(*a), nil
}

func (s *service) Create(ctx context.Context, userID, plan string, req CreateAreaRequest) (AreaView, error) {
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		return AreaView{}, apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidInput, "name is required")
	}
	if len(req.Name) > 255 {
		return AreaView{}, apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidInput, "name must be 255 characters or fewer")
	}

	if req.Color == "" {
		req.Color = defaultColor
	} else if !colorRE.MatchString(req.Color) {
		return AreaView{}, apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidInput, "color must be a valid hex color (e.g. #3B82F6)")
	}

	if req.Icon == "" {
		req.Icon = defaultIcon
	} else if !project.AllowedIcons[req.Icon] {
		return AreaView{}, apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidInput, "icon is not valid")
	}

	if plan == "free" {
		count, err := s.repo.CountByUser(ctx, userID)
		if err != nil {
			return AreaView{}, fmt.Errorf("area.Create count: %w", err)
		}
		if count >= freePlanAreaLimit {
			return AreaView{}, apperror.New(http.StatusForbidden, apperror.ErrPlanLimitExceeded, "free plan allows up to 3 areas")
		}
	}

	a, err := s.repo.Create(ctx, Area{
		ID:     uuid.New().String(),
		UserID: userID,
		Name:   req.Name,
		Color:  req.Color,
		Icon:   req.Icon,
	})
	if err != nil {
		return AreaView{}, err
	}
	view := areaToView(a)
	s.emit(userID, Event{Type: EventCreated, Payload: view})
	return view, nil
}

func (s *service) Update(ctx context.Context, userID, id string, req UpdateAreaRequest) (AreaView, error) {
	if req.Name != nil {
		*req.Name = strings.TrimSpace(*req.Name)
		if *req.Name == "" {
			return AreaView{}, apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidInput, "name cannot be empty")
		}
		if len(*req.Name) > 255 {
			return AreaView{}, apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidInput, "name must be 255 characters or fewer")
		}
	}
	if req.Color != nil && !colorRE.MatchString(*req.Color) {
		return AreaView{}, apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidInput, "color must be a valid hex color (e.g. #3B82F6)")
	}
	if req.Icon != nil && !project.AllowedIcons[*req.Icon] {
		return AreaView{}, apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidInput, "icon is not valid")
	}

	a, err := s.repo.Update(ctx, userID, id, req)
	if err != nil {
		return AreaView{}, err
	}
	view := areaToView(a)
	s.emit(userID, Event{Type: EventUpdated, Payload: view})
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
