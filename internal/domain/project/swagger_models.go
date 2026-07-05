package project

// Swagger documentation models. These mirror the runtime response envelope
// (pkg/respond) so swag can reference exported types; they are never instantiated.

// SwaggerError is the structured error body returned in the envelope.
type SwaggerError struct {
	Code    string `json:"code" example:"PROJECT_NOT_FOUND"`
	Message string `json:"message" example:"project not found"`
}

// ErrorEnvelope is a failed response: data is null, error is populated.
type ErrorEnvelope struct {
	Data  interface{}  `json:"data"`
	Error SwaggerError `json:"error"`
}

// ProjectEnvelope wraps a single-project success response.
type ProjectEnvelope struct {
	Data  ProjectView `json:"data"`
	Error interface{} `json:"error"`
}

// ProjectListEnvelope wraps a paginated list of projects.
type ProjectListEnvelope struct {
	Data  ListProjectsResponse `json:"data"`
	Error interface{}          `json:"error"`
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
