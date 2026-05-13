package middleware

import (
	"net/http"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
	"github.com/nicoflow/nicoflow-api/pkg/respond"
)

// PlanEnforcer rejects users whose plan does not match requiredTier.
// Must be placed after Auth middleware so PlanFromCtx is populated.
func PlanEnforcer(requiredTier string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if PlanFromCtx(r.Context()) != requiredTier {
				respond.Error(w, http.StatusForbidden, apperror.ErrPlanLimitExceeded, "plan upgrade required")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
