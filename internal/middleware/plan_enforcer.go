package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// PlanEnforcer rejects free-tier users from routes that require a paid plan.
func PlanEnforcer(requiredTier string) gin.HandlerFunc {
	return func(c *gin.Context) {
		plan := c.GetString(ContextPlan)
		if plan != requiredTier {
			c.AbortWithStatusJSON(http.StatusPaymentRequired, gin.H{"error": "plan upgrade required"})
			return
		}
		c.Next()
	}
}
