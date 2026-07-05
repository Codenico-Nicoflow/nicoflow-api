package area

// Swagger documentation models. These mirror the runtime response envelope
// (pkg/respond) so swag can reference exported types; they are never instantiated.

// SwaggerError is the structured error body returned in the envelope.
type SwaggerError struct {
	Code    string `json:"code" example:"AREA_NOT_FOUND"`
	Message string `json:"message" example:"area not found"`
}

// ErrorEnvelope is a failed response: data is null, error is populated.
type ErrorEnvelope struct {
	Data  interface{}  `json:"data"`
	Error SwaggerError `json:"error"`
}

// AreaEnvelope wraps a single-area success response.
type AreaEnvelope struct {
	Data  AreaView    `json:"data"`
	Error interface{} `json:"error"`
}

// AreaListEnvelope wraps a paginated list of areas.
type AreaListEnvelope struct {
	Data  ListAreasResponse `json:"data"`
	Error interface{}       `json:"error"`
}

// AreaWithProjectsEnvelope wraps the areas-with-projects response.
type AreaWithProjectsEnvelope struct {
	Data  []AreaWithProjectsView `json:"data"`
	Error interface{}            `json:"error"`
}

// ReorderResultEnvelope wraps the { updated: n } reorder response.
type ReorderResultEnvelope struct {
	Data  ReorderResult `json:"data"`
	Error interface{}   `json:"error"`
}

// ReorderResult is the payload of a reorder response.
type ReorderResult struct {
	Updated int `json:"updated" example:"3"`
}
