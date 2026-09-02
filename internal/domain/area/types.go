package area

import (
	"time"

	"github.com/nicoflow/nicoflow-api/internal/domain/project"
)

// Area is the internal domain model.
type Area struct {
	ID           string
	UserID       string
	Name         string
	Color        string
	Icon         string
	DisplayOrder int
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// AreaWithProjects is the internal model used for the with-projects query.
type AreaWithProjects struct {
	Area
	Projects []project.Project
}

// AreaView is the JSON response shape for a single area.
type AreaView struct {
	ID           string `json:"id" validate:"required"`
	Name         string `json:"name" validate:"required"`
	Color        string `json:"color" validate:"required"`
	Icon         string `json:"icon" validate:"required"`
	DisplayOrder int    `json:"displayOrder" validate:"required"`
	CreatedAt    string `json:"createdAt" format:"date-time" validate:"required"`
	UpdatedAt    string `json:"updatedAt" format:"date-time" validate:"required"`
}

// AreaWithProjectsView is the JSON response shape for an area with its nested projects.
type AreaWithProjectsView struct {
	AreaView
	Projects []project.ProjectView `json:"projects"`
}

// ListAreasResponse is the paginated list response.
type ListAreasResponse struct {
	Items      []AreaView `json:"items"`
	NextCursor string     `json:"nextCursor"`
}

// CreateAreaRequest is the body for POST /areas.
type CreateAreaRequest struct {
	Name  string `json:"name"`
	Color string `json:"color"`
	Icon  string `json:"icon"`
}

// UpdateAreaRequest is the body for PATCH /areas/:id — all fields optional.
type UpdateAreaRequest struct {
	Name  *string `json:"name"`
	Color *string `json:"color"`
	Icon  *string `json:"icon"`
}

// ReorderItem is a single entry in a reorder request.
type ReorderItem struct {
	ID           string `json:"id"`
	DisplayOrder int    `json:"displayOrder"`
}

// ReorderRequest is the body for PATCH /areas/reorder.
type ReorderRequest struct {
	Items []ReorderItem `json:"items"`
}

// ListAreasFilter holds parsed query parameters for the area list endpoint.
type ListAreasFilter struct {
	Query  string
	Limit  int
	Cursor string
}

func areaToView(a Area) AreaView {
	return AreaView{
		ID:           a.ID,
		Name:         a.Name,
		Color:        a.Color,
		Icon:         a.Icon,
		DisplayOrder: a.DisplayOrder,
		CreatedAt:    a.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:    a.UpdatedAt.UTC().Format(time.RFC3339),
	}
}
