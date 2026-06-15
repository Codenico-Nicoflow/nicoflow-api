package auth

// Swagger documentation models. These mirror the runtime response envelope
// (pkg/respond) so swag can reference exported types; they are never instantiated.

// SwaggerError is the structured error body returned in the envelope.
type SwaggerError struct {
	Code    string `json:"code" example:"UNAUTHORIZED"`
	Message string `json:"message" example:"invalid email or password"`
}

// ErrorEnvelope is a failed response: data is null, error is populated.
type ErrorEnvelope struct {
	Data  interface{}  `json:"data"`
	Error SwaggerError `json:"error"`
}

// AuthEnvelope wraps a successful login/register/refresh response.
type AuthEnvelope struct {
	Data  AuthResponse `json:"data"`
	Error interface{}  `json:"error"`
}

// UserEnvelope wraps a successful profile/update response.
type UserEnvelope struct {
	Data  UserView    `json:"data"`
	Error interface{} `json:"error"`
}

// MessageEnvelope wraps a success response carrying a human-readable message.
type MessageEnvelope struct {
	Data  MessageData `json:"data"`
	Error interface{} `json:"error"`
}

// MessageData is the {message} payload returned by forgot/reset/verify endpoints.
type MessageData struct {
	Message string `json:"message" example:"password updated successfully"`
}
