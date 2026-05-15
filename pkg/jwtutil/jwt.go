package jwtutil

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const issuer = "nicoflow-api"

// Claims is the canonical JWT claims struct for nicoflow-api.
// sub = user ID, email and plan avoid per-request DB lookups.
type Claims struct {
	jwt.RegisteredClaims
	Email string `json:"email"`
	Plan  string `json:"plan"`
}

// Issue signs a new HS256 JWT with the given identity and TTL.
func Issue(userID, email, plan, secret string, ttl time.Duration) (string, error) {
	now := time.Now()
	c := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			Issuer:    issuer,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
		},
		Email: email,
		Plan:  plan,
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, c)
	signed, err := token.SignedString([]byte(secret))
	if err != nil {
		return "", fmt.Errorf("jwtutil.Issue: %w", err)
	}
	return signed, nil
}

// Parse validates the token string and returns the parsed Claims.
// Returns an error if the signature, expiry, or issuer is invalid.
func Parse(tokenStr, secret string) (*Claims, error) {
	parsed, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return []byte(secret), nil
	}, jwt.WithIssuer(issuer), jwt.WithExpirationRequired()) //nolint:wrapcheck
	if err != nil {
		return nil, fmt.Errorf("jwtutil.Parse: %w", err)
	}

	claims, ok := parsed.Claims.(*Claims)
	if !ok || !parsed.Valid {
		return nil, errors.New("jwtutil.Parse: invalid claims")
	}
	if claims.Subject == "" {
		return nil, errors.New("jwtutil.Parse: missing subject")
	}
	return claims, nil
}
