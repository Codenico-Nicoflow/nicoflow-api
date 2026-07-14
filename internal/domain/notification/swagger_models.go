package notification

// Swagger documentation models. These mirror the runtime response envelope
// (pkg/respond) so swag can reference exported types; they are never instantiated.

// SwaggerError is the structured error body returned in the envelope.
type SwaggerError struct {
	Code    string `json:"code" example:"NOTIFICATION_NOT_FOUND"`
	Message string `json:"message" example:"notification not found"`
}

// ErrorEnvelope is a failed response: data is null, error is populated.
type ErrorEnvelope struct {
	Data  interface{}  `json:"data"`
	Error SwaggerError `json:"error"`
}

// NotificationEnvelope wraps a single-notification success response.
type NotificationEnvelope struct {
	Data  NotificationView `json:"data"`
	Error interface{}      `json:"error"`
}

// NotificationListEnvelope wraps a paginated list of notifications.
type NotificationListEnvelope struct {
	Data  ListNotificationsResponse `json:"data"`
	Error interface{}               `json:"error"`
}

// UnreadCountEnvelope wraps the { count: n } unread-count response.
type UnreadCountEnvelope struct {
	Data  UnreadCountResponse `json:"data"`
	Error interface{}         `json:"error"`
}

// CountEnvelope wraps the { count: n } mark-all-read response.
type CountEnvelope struct {
	Data  CountResponse `json:"data"`
	Error interface{}   `json:"error"`
}
