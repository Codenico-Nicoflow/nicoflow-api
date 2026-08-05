package habit

// Swagger documentation models. These mirror the runtime response envelope
// (pkg/respond) so swag can reference exported types; they are never instantiated.

// SwaggerError is the structured error body returned in the envelope.
type SwaggerError struct {
	Code    string `json:"code" example:"HABIT_NOT_FOUND"`
	Message string `json:"message" example:"habit not found"`
}

// ErrorEnvelope is a failed response: data is null, error is populated.
type ErrorEnvelope struct {
	Data  interface{}  `json:"data"`
	Error SwaggerError `json:"error"`
}

// HabitEnvelope wraps a single habit.
type HabitEnvelope struct {
	Data  HabitView   `json:"data"`
	Error interface{} `json:"error"`
}

// HabitDetailEnvelope wraps a single habit read. Same shape as HabitEnvelope —
// the scalar and list reads differ only in how much of the `cells` window they
// carry — kept as its own name so the endpoint docs stay readable.
type HabitDetailEnvelope struct {
	Data  HabitView   `json:"data"`
	Error interface{} `json:"error"`
}

// SubjectListEnvelope wraps the subject catalog.
type SubjectListEnvelope struct {
	Data  []SubjectView `json:"data"`
	Error interface{}   `json:"error"`
}

// HabitListEnvelope wraps the caller's habits.
type HabitListEnvelope struct {
	Data  []HabitView `json:"data"`
	Error interface{} `json:"error"`
}
