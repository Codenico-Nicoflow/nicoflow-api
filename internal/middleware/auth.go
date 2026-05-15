package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
	"github.com/nicoflow/nicoflow-api/pkg/jwtutil"
	"github.com/nicoflow/nicoflow-api/pkg/respond"
)

type ctxKeyUserID struct{}
type ctxKeyPlan struct{}
type ctxKeyEmail struct{}

// Auth validates the Bearer JWT and injects userID, email, and plan into the request context.
func Auth(jwtSecret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			header := r.Header.Get("Authorization")
			if !strings.HasPrefix(header, "Bearer ") {
				respond.Error(w, http.StatusUnauthorized, apperror.ErrUnauthorized, "missing or malformed token")
				return
			}
			tokenStr := strings.TrimPrefix(header, "Bearer ")

			claims, err := jwtutil.Parse(tokenStr, jwtSecret)
			if err != nil {
				respond.Error(w, http.StatusUnauthorized, apperror.ErrInvalidToken, "invalid or expired token")
				return
			}

			ctx := context.WithValue(r.Context(), ctxKeyUserID{}, claims.Subject)
			ctx = context.WithValue(ctx, ctxKeyPlan{}, claims.Plan)
			ctx = context.WithValue(ctx, ctxKeyEmail{}, claims.Email)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// UserIDFromCtx returns the authenticated user's ID from the request context.
func UserIDFromCtx(ctx context.Context) string {
	id, _ := ctx.Value(ctxKeyUserID{}).(string)
	return id
}

// PlanFromCtx returns the authenticated user's plan from the request context.
func PlanFromCtx(ctx context.Context) string {
	plan, _ := ctx.Value(ctxKeyPlan{}).(string)
	return plan
}

// EmailFromCtx returns the authenticated user's email from the request context.
func EmailFromCtx(ctx context.Context) string {
	email, _ := ctx.Value(ctxKeyEmail{}).(string)
	return email
}
