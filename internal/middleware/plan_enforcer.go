package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"nicoflow-api/internal/response"
)

// PlanEnforcer rejects free-tier users from routes that require a paid plan.
func PlanEnforcer(requiredTier string) gin.HandlerFunc {
	return func(c *gin.Context) {
		plan := c.GetString(ContextPlan)
		if plan != requiredTier {
			response.RespondError(c, http.StatusForbidden, response.ErrPlanLimitExceeded, "plan upgrade required")
			c.Abort()
			return
		}
		c.Next()
	}
}
