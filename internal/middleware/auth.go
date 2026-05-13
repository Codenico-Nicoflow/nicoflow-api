package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
	"github.com/nicoflow/nicoflow-api/pkg/respond"
)

type ctxKeyUserID struct{}
type ctxKeyPlan struct{}

type claims struct {
	UserID string `json:"userId"`
	Plan   string `json:"plan"`
	jwt.RegisteredClaims
}

// Auth validates the Bearer JWT and injects userID + plan into the request context.
func Auth(jwtSecret string) func(http.Handler) http.Handler {
	key := []byte(jwtSecret)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			header := r.Header.Get("Authorization")
			if !strings.HasPrefix(header, "Bearer ") {
				respond.Error(w, http.StatusUnauthorized, apperror.ErrUnauthorized, "missing or malformed token")
				return
			}
			tokenStr := strings.TrimPrefix(header, "Bearer ")

			parsed, err := jwt.ParseWithClaims(tokenStr, &claims{}, func(t *jwt.Token) (interface{}, error) {
				if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
					return nil, jwt.ErrSignatureInvalid
				}
				return key, nil
			})
			if err != nil || !parsed.Valid {
				respond.Error(w, http.StatusUnauthorized, apperror.ErrInvalidToken, "invalid or expired token")
				return
			}

			cl, ok := parsed.Claims.(*claims)
			if !ok || cl.UserID == "" {
				respond.Error(w, http.StatusUnauthorized, apperror.ErrInvalidToken, "invalid token claims")
				return
			}

			ctx := context.WithValue(r.Context(), ctxKeyUserID{}, cl.UserID)
			ctx = context.WithValue(ctx, ctxKeyPlan{}, cl.Plan)
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
