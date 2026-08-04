package search

// Swagger documentation models. These mirror the runtime response envelope
// (pkg/respond) so swag can reference exported types; they are never instantiated.
// The runtime envelope itself is unexported, hence the per-domain mirror.

// SwaggerError is the structured error body returned in the envelope.
type SwaggerError struct {
	Code    string `json:"code" example:"INVALID_INPUT"`
	Message string `json:"message" example:"query is required"`
}

// ErrorEnvelope is a failed response: data is null, error is populated.
type ErrorEnvelope struct {
	Data  interface{}  `json:"data"`
	Error SwaggerError `json:"error"`
}

// SearchEnvelope wraps the grouped search results.
type SearchEnvelope struct {
	Data  Response    `json:"data"`
	Error interface{} `json:"error"`
}
