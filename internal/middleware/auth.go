package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"

	"nicoflow-api/internal/response"
)

const (
	ContextUserID = "userID"
	ContextPlan   = "plan"
)

// Claims is basically the JWT payload
type claims struct {
	UserID string `json:"userId"`
	Plan   string `json:"plan"`
	jwt.RegisteredClaims
}

func Auth(jwtSecret string) gin.HandlerFunc {
	key := []byte(jwtSecret)
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		if !strings.HasPrefix(header, "Bearer ") {
			response.RespondError(c, http.StatusUnauthorized, response.ErrInvalidToken, "missing or malformed token")
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
			response.RespondError(c, http.StatusUnauthorized, response.ErrInvalidToken, "invalid or expired token")
			return
		}

		cl, ok := parsed.Claims.(*claims)
		if !ok || cl.UserID == "" {
			response.RespondError(c, http.StatusUnauthorized, response.ErrInvalidToken, "invalid token claims")
			return
		}

		c.Set(ContextUserID, cl.UserID)
		c.Set(ContextPlan, cl.Plan)
		c.Next()
	}
}
