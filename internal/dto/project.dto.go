package dto

// TODO: implement
type CreateProjectRequest struct {
	Name   string  `json:"name" binding:"required"`
	AreaID *string `json:"areaId"`
}

type UpdateProjectRequest struct {
	Name   string  `json:"name" binding:"required"`
	AreaID *string `json:"areaId"`
}
