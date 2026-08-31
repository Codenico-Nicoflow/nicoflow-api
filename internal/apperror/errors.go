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
	ErrInvalidInput           = "INVALID_INPUT"
	ErrInvalidToken           = "INVALID_TOKEN"
	ErrUnauthorized           = "UNAUTHORIZED"
	ErrEmailNotVerified       = "EMAIL_NOT_VERIFIED"
	ErrForbidden              = "FORBIDDEN"
	ErrPlanLimitExceeded      = "PLAN_LIMIT_EXCEEDED"
	ErrStorageLimitExceeded   = "STORAGE_LIMIT_EXCEEDED"
	ErrPermissionDenied       = "PERMISSION_DENIED"
	ErrResourceNotFound       = "RESOURCE_NOT_FOUND"
	ErrTaskNotFound           = "TASK_NOT_FOUND"
	ErrProjectNotFound        = "PROJECT_NOT_FOUND"
	ErrAreaNotFound           = "AREA_NOT_FOUND"
	ErrUserNotFound           = "USER_NOT_FOUND"
	ErrSessionNotFound        = "SESSION_NOT_FOUND"
	ErrMessageNotFound        = "MESSAGE_NOT_FOUND"
	ErrNotificationNotFound   = "NOTIFICATION_NOT_FOUND"
	ErrRecurrenceRuleNotFound = "RECURRENCE_RULE_NOT_FOUND"
	ErrHabitNotFound          = "HABIT_NOT_FOUND"
	ErrConflict               = "CONFLICT"
	ErrEmailAlreadyExists     = "EMAIL_ALREADY_EXISTS"
	ErrUsernameAlreadyExists  = "USERNAME_ALREADY_EXISTS"
	ErrDuplicateName          = "DUPLICATE_NAME"
	ErrIdempotencyConflict    = "IDEMPOTENCY_CONFLICT"
	ErrRateLimited            = "RATE_LIMITED"
	ErrAILimitReached         = "AI_LIMIT_REACHED"
	// AI foundation (E-026 / NIC-1681):
	// AI_UNAVAILABLE (503) — feature disabled (no key), provider 429·529, or first-token timeout.
	// AI_PROVIDER_ERROR (502) — provider 400·401: our fault, logged loud with request_id.
	// AI_STREAM_ACTIVE (409) — a stream is already in flight for the session.
	ErrAIUnavailable   = "AI_UNAVAILABLE"
	ErrAIProviderError = "AI_PROVIDER_ERROR"
	ErrAIStreamActive  = "AI_STREAM_ACTIVE"
	// Google Calendar (E-052 / NIC-1844):
	// GOOGLE_NOT_CONNECTED (409) — no connection for this user; the client prompts to connect.
	// GOOGLE_AUTH_FAILED (502) — token exchange, revoke, or stored-credential read failed.
	// Neither ever carries token material in its message.
	ErrGoogleNotConnected = "GOOGLE_NOT_CONNECTED"
	ErrGoogleAuthFailed   = "GOOGLE_AUTH_FAILED"
	ErrInvalidProjectId   = "INVALID_PROJECT_ID"
	ErrInvalidStatus      = "INVALID_STATUS"
	// TASK_NOT_MISSABLE (422) — mark-missed rejected: not a recurring occurrence,
	// already terminal (done/cancelled), or its occurrence date is still in the
	// future relative to the owner's local today.
	ErrTaskNotMissable = "TASK_NOT_MISSABLE"
	// INVALID_RECURRENCE (422) — a malformed schedule: bad freq, interval outside
	// 1..366, weekday/monthday out of range, or a field set on the wrong freq.
	ErrInvalidRecurrence = "INVALID_RECURRENCE"
	// TASK_ALREADY_RECURRING (409) — convert-to-recurring rejected: the task
	// already belongs to a rule. Edit that rule instead (PATCH
	// /recurrence-rules/:id), never a second convert on the same task.
	ErrTaskAlreadyRecurring = "TASK_ALREADY_RECURRING"
	// TASK_NOT_SKIPPABLE (409) — Skip rejected: not a recurring occurrence, not
	// the live instance (already reaped missed/skipped), or not active
	// (NIC-1997). Preserved for backward compat; callers now get one of the
	// fine-grained codes below instead.
	ErrTaskNotSkippable = "TASK_NOT_SKIPPABLE"
	// Fine-grained Skip rejection codes (NIC-2000 hardening pass). All 409.
	ErrTaskNotRecurring     = "TASK_NOT_RECURRING"     // task has no recurrence rule
	ErrTaskAlreadySkipped   = "TASK_ALREADY_SKIPPED"   // occurrence_status = 'skipped'
	ErrTaskAlreadyMissed    = "TASK_ALREADY_MISSED"    // occurrence_status = 'missed'
	ErrTaskAlreadyPaused    = "TASK_ALREADY_PAUSED"    // occurrence_status = 'paused'
	ErrTaskAlreadyCancelled = "TASK_ALREADY_CANCELLED" // occurrence_status = 'cancelled' (legacy)
	ErrTaskNotActive        = "TASK_NOT_ACTIVE"        // status != 'active'
	// TASK_RECURRING_NOT_RESCHEDULABLE (409) — Schedule rejected: cannot manually
	// reschedule a live recurring occurrence (desyncs scheduled_for from
	// occurrence_date and causes the sweep to unjustly reap it as missed).
	ErrTaskRecurringNotReschedulable = "TASK_RECURRING_NOT_RESCHEDULABLE"
	// TASK_RECURRING_NOT_REVERSIBLE (409) — un-complete rejected: a completed
	// recurring occurrence cannot be reopened because its successor was already
	// created when it was completed.
	ErrTaskRecurringNotReversible = "TASK_RECURRING_NOT_REVERSIBLE"
	// RECURRING_LIVE_INSTANCE (409) — Delete rejected: the task is the still-live
	// instance of a recurring series. Skip the occurrence or end the series
	// instead of hard-deleting it out from under the recurrence engine
	// (NIC-1997).
	ErrRecurringLiveInstance = "RECURRING_LIVE_INSTANCE"
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
