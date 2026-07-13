package apperror

// AppError is the standard error type returned by service and repository layers.
// Handlers convert it to a JSON response via pkg/respond.Error.
type AppError struct {
	Code    string
	Message string
	Status  int
}

func (e *AppError) Error() string { return e.Message }

// New creates a new AppError.
func New(status int, code, message string) *AppError {
	return &AppError{Status: status, Code: code, Message: message}
}

// Error code constants — match SPEC §4 exactly.
const (
	ErrInvalidInput          = "INVALID_INPUT"
	ErrInvalidToken          = "INVALID_TOKEN"
	ErrUnauthorized          = "UNAUTHORIZED"
	ErrEmailNotVerified      = "EMAIL_NOT_VERIFIED"
	ErrForbidden             = "FORBIDDEN"
	ErrPlanLimitExceeded     = "PLAN_LIMIT_EXCEEDED"
	ErrPermissionDenied      = "PERMISSION_DENIED"
	ErrResourceNotFound      = "RESOURCE_NOT_FOUND"
	ErrTaskNotFound          = "TASK_NOT_FOUND"
	ErrProjectNotFound       = "PROJECT_NOT_FOUND"
	ErrAreaNotFound          = "AREA_NOT_FOUND"
	ErrUserNotFound          = "USER_NOT_FOUND"
	ErrSessionNotFound       = "SESSION_NOT_FOUND"
	ErrMessageNotFound       = "MESSAGE_NOT_FOUND"
	ErrNotificationNotFound  = "NOTIFICATION_NOT_FOUND"
	ErrConflict              = "CONFLICT"
	ErrEmailAlreadyExists    = "EMAIL_ALREADY_EXISTS"
	ErrUsernameAlreadyExists = "USERNAME_ALREADY_EXISTS"
	ErrDuplicateName         = "DUPLICATE_NAME"
	ErrIdempotencyConflict   = "IDEMPOTENCY_CONFLICT"
	ErrRateLimited           = "RATE_LIMITED"
	ErrAILimitReached        = "AI_LIMIT_REACHED"
	ErrInvalidProjectId      = "INVALID_PROJECT_ID"
	ErrInvalidStatus         = "INVALID_STATUS"
	ErrInvalidDate           = "INVALID_DATE"
	ErrInvalidPriority       = "INVALID_PRIORITY"
	ErrInvalidAIContext      = "INVALID_AI_CONTEXT"
	ErrInvalidEmail          = "INVALID_EMAIL"
	ErrWeakPassword          = "WEAK_PASSWORD"
	ErrRequired              = "REQUIRED"
	ErrDatabaseError         = "DATABASE_ERROR"
	ErrInternalServerError   = "INTERNAL_SERVER_ERROR"
	ErrServiceUnavailable    = "SERVICE_UNAVAILABLE"
)
