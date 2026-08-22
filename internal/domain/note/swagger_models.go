package note

// Swagger documentation models. These mirror the runtime response envelope
// (pkg/respond) so swag can reference exported types; they are never instantiated.

// SwaggerError is the structured error body returned in the envelope.
type SwaggerError struct {
	Code    string `json:"code" example:"RESOURCE_NOT_FOUND"`
	Message string `json:"message" example:"note not found"`
}

// ErrorEnvelope is a failed response: data is null, error is populated.
type ErrorEnvelope struct {
	Data  interface{}  `json:"data"`
	Error SwaggerError `json:"error"`
}

// NoteDetailEnvelope wraps a single note including its full document.
type NoteDetailEnvelope struct {
	Data  NoteDetailView `json:"data"`
	Error interface{}    `json:"error"`
}

// NoteListEnvelope wraps the excerpt-only list of a project's notes.
type NoteListEnvelope struct {
	Data  []NoteView  `json:"data"`
	Error interface{} `json:"error"`
}

// MentionSearchEnvelope wraps the @-mention typeahead results.
type MentionSearchEnvelope struct {
	Data  []MentionResult `json:"data"`
	Error interface{}     `json:"error"`
}
