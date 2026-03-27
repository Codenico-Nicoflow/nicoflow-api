package response

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

const (
	ErrInvalidInput        = "INVALID_INPUT"        // 400
	ErrInvalidToken        = "INVALID_TOKEN"        // 401
	ErrUnauthorized        = "UNAUTHORIZED"         // 401
	ErrForbidden           = "FORBIDDEN"            // 403
	ErrPlanLimitExceeded   = "PLAN_LIMIT_EXCEEDED"  // 403
	ErrResourceNotFound    = "RESOURCE_NOT_FOUND"   // 404
	ErrConflict            = "CONFLICT"             // 409
	ErrTaskNotFound        = "TASK_NOT_FOUND"       // 404
	ErrProjectNotFound     = "PROJECT_NOT_FOUND"    // 404
	ErrAreaNotFound        = "AREA_NOT_FOUND"       // 404
	ErrUserNotFound        = "USER_NOT_FOUND"       // 404
	ErrInvalidProjectId    = "INVALID_PROJECT_ID"   // 400
	ErrDuplicateName       = "DUPLICATE_NAME"       // 409
	ErrInvalidStatus       = "INVALID_STATUS"       // 400
	ErrInvalidDate         = "INVALID_DATE"         // 400
	ErrInvalidPriority     = "INVALID_PRIORITY"     // 400
	ErrDatabaseError       = "DATABASE_ERROR"       // 500
	ErrServiceUnavailable  = "SERVICE_UNAVAILABLE"  // 503
	ErrRateLimited         = "RATE_LIMITED"         // 429
	ErrInvalidEmail        = "INVALID_EMAIL"        // 400
	ErrEmailAlreadyExists  = "EMAIL_ALREADY_EXISTS" // 409
	ErrWeakPassword        = "WEAK_PASSWORD"        // 400
	ErrIdempotencyConflict = "IDEMPOTENCY_CONFLICT" // 409
	ErrPermissionDenied    = "PERMISSION_DENIED"    // 403
	ErrSessionNotFound     = "SESSION_NOT_FOUND"    // 404
	ErrMessageNotFound     = "MESSAGE_NOT_FOUND"    // 404
	ErrInvalidAIContext    = "INVALID_AI_CONTEXT"   // 400
	ErrAILimitReached      = "AI_LIMIT_REACHED"     // 403
)

type envelope struct {
	Data  interface{} `json:"data,omitempty"`
	Error string      `json:"error,omitempty"`
}

func RespondOK(c *gin.Context, data any) {
	c.JSON(http.StatusOK, gin.H{"data": data, "error": nil})
}
func RespondCreated(c *gin.Context, data any) {
	c.JSON(http.StatusCreated, gin.H{"data": data, "error": nil})
}

func RespondError(c *gin.Context, status int, code, message string) {
	c.JSON(status, gin.H{
		"data": nil,
		"error": gin.H{
			"code":    code,
			"message": message,
		},
	})
	c.Abort()
}
