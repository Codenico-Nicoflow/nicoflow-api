package googlecal

// Swagger documentation models. These mirror the runtime response envelope
// (pkg/respond) so swag can reference exported types; they are never instantiated.

// SwaggerError is the structured error body returned in the envelope.
type SwaggerError struct {
	Code    string `json:"code" example:"GOOGLE_NOT_CONNECTED"`
	Message string `json:"message" example:"google calendar is not connected"`
}

// ErrorEnvelope is a failed response: data is null, error is populated.
type ErrorEnvelope struct {
	Data  interface{}  `json:"data"`
	Error SwaggerError `json:"error"`
}
