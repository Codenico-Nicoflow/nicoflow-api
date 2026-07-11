package project

import (
	"time"

	"github.com/nicoflow/nicoflow-api/pkg/optional"
)

// Project is the internal domain model.
type Project struct {
	ID           string
	UserID       string
	AreaID       string
	Name         string
	Status       string
	FolderIcon   string
	DueDate      *time.Time
	IsFavorite   bool
	Description  *string
	DisplayOrder int
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// ProjectView is the JSON response shape for a single project.
type ProjectView struct {
	ID           string  `json:"id"`
	AreaID       string  `json:"areaId"`
	Name         string  `json:"name"`
	Status       string  `json:"status"`
	FolderIcon   string  `json:"folderIcon"`
	DueDate      *string `json:"dueDate"`
	IsFavorite   bool    `json:"isFavorite"`
	Description  *string `json:"description"`
	DisplayOrder int     `json:"displayOrder"`
	CreatedAt    string  `json:"createdAt"`
	UpdatedAt    string  `json:"updatedAt"`
}

// ListProjectsResponse is the paginated list response.
type ListProjectsResponse struct {
	Items      []ProjectView `json:"items"`
	NextCursor string        `json:"nextCursor"`
}

// CreateProjectRequest is the body for POST /areas/:areaId/projects.
type CreateProjectRequest struct {
	Name        string     `json:"name"`
	Status      string     `json:"status"`
	FolderIcon  string     `json:"folderIcon"`
	DueDate     *time.Time `json:"dueDate"`
	IsFavorite  bool       `json:"isFavorite"`
	Description *string    `json:"description"`
}

// UpdateProjectRequest is the body for PATCH /projects/:id — all fields optional.
type UpdateProjectRequest struct {
	Name        *string                   `json:"name"`
	AreaID      *string                   `json:"areaId"`
	Status      *string                   `json:"status"`
	FolderIcon  *string                   `json:"folderIcon"`
	IsFavorite  *bool                     `json:"isFavorite"`
	DueDate     optional.Field[time.Time] `json:"dueDate"`
	Description optional.Field[string]    `json:"description"`
}

// ReorderItem is a single entry in a reorder request.
type ReorderItem struct {
	ID           string `json:"id"`
	DisplayOrder int    `json:"displayOrder"`
}

// ReorderRequest is the body for PATCH /projects/reorder.
type ReorderRequest struct {
	Items []ReorderItem `json:"items"`
}

// ListProjectsFilter holds parsed query parameters for project list endpoints.
type ListProjectsFilter struct {
	AreaID     *string
	Status     *string
	IsFavorite *bool
	Query      string
	Limit      int
	Cursor     string
}

func ProjectToView(p Project) ProjectView {
	v := ProjectView{
		ID:           p.ID,
		AreaID:       p.AreaID,
		Name:         p.Name,
		Status:       p.Status,
		FolderIcon:   p.FolderIcon,
		IsFavorite:   p.IsFavorite,
		Description:  p.Description,
		DisplayOrder: p.DisplayOrder,
		CreatedAt:    p.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:    p.UpdatedAt.UTC().Format(time.RFC3339),
	}
	if p.DueDate != nil {
		s := p.DueDate.UTC().Format(time.RFC3339)
		v.DueDate = &s
	}
	return v
}
