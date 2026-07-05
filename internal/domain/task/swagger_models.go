package task

// Swagger documentation models. These mirror the runtime response envelope
// (pkg/respond) so swag can reference exported types; they are never instantiated.

// SwaggerError is the structured error body returned in the envelope.
type SwaggerError struct {
	Code    string `json:"code" example:"TASK_NOT_FOUND"`
	Message string `json:"message" example:"task not found"`
}

// ErrorEnvelope is a failed response: data is null, error is populated.
type ErrorEnvelope struct {
	Data  interface{}  `json:"data"`
	Error SwaggerError `json:"error"`
}

// TaskEnvelope wraps a single-task success response.
type TaskEnvelope struct {
	Data  TaskView    `json:"data"`
	Error interface{} `json:"error"`
}

// TaskListEnvelope wraps a list of tasks ({ items: [...] }).
type TaskListEnvelope struct {
	Data  ListTasksResponse `json:"data"`
	Error interface{}       `json:"error"`
}

// SubtaskEnvelope wraps a single-subtask success response.
type SubtaskEnvelope struct {
	Data  SubtaskView `json:"data"`
	Error interface{} `json:"error"`
}

// SubtaskListEnvelope wraps a list of subtasks ({ items: [...] }).
type SubtaskListEnvelope struct {
	Data  ListSubtasksResponse `json:"data"`
	Error interface{}          `json:"error"`
}

// TimeSpreadEnvelope wraps the today/tomorrow/thisWeek bucket response.
type TimeSpreadEnvelope struct {
	Data  TimeSpreadResponse `json:"data"`
	Error interface{}        `json:"error"`
}
