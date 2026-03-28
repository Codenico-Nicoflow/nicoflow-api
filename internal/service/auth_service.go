package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"nicoflow-api/internal/model"
	"nicoflow-api/internal/repository"
)

var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrEmailTaken         = errors.New("email already in use")
	ErrTokenExpired       = errors.New("refresh token expired")
	ErrTokenNotFound      = errors.New("refresh token not found")
)

const (
	accessTokenTTL  = 15 * time.Minute
	refreshTokenTTL = 7 * 24 * time.Hour
)

type jwtClaims struct {
	UserID string `json:"userId"`
	Plan   string `json:"plan"`
	jwt.RegisteredClaims
}

type AuthService struct {
	users         repository.UserRepo
	refreshTokens repository.RefreshTokenRepo
	jwtSecret     string
}

func NewAuthService(users repository.UserRepo, refreshTokens repository.RefreshTokenRepo, jwtSecret string) *AuthService {
	return &AuthService{users: users, refreshTokens: refreshTokens, jwtSecret: jwtSecret}
}

func (s *AuthService) Register(ctx context.Context, email, password string) (*model.User, error) {
	existing, err := s.users.FindByEmail(ctx, email)
	if err != nil {
		return nil, fmt.Errorf("AuthService.Register: %w", err)
	}
	if existing != nil {
		return nil, ErrEmailTaken
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("AuthService.Register: hash password: %w", err)
	}

	now := time.Now().UTC()
	u := &model.User{
		ID:           uuid.NewString(),
		Email:        email,
		PasswordHash: string(hash),
		Timezone:     "UTC",
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := s.users.Create(ctx, u); err != nil {
		return nil, fmt.Errorf("AuthService.Register: %w", err)
	}
	return u, nil
}

func (s *AuthService) Login(ctx context.Context, email, password string) (accessToken, refreshToken string, err error) {
	u, err := s.users.FindByEmail(ctx, email)
	if err != nil {
		return "", "", fmt.Errorf("AuthService.Login: %w", err)
	}
	if u == nil {
		return "", "", ErrInvalidCredentials
	}

	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)); err != nil {
		return "", "", ErrInvalidCredentials
	}

	accessToken, err = s.issueAccessToken(u.ID, "free")
	if err != nil {
		return "", "", fmt.Errorf("AuthService.Login: %w", err)
	}

	refreshToken, err = s.issueRefreshToken(ctx, u.ID)
	if err != nil {
		return "", "", fmt.Errorf("AuthService.Login: %w", err)
	}
	return accessToken, refreshToken, nil
}

func (s *AuthService) Refresh(ctx context.Context, rawToken string) (string, error) {
	hash := hashToken(rawToken)
	t, err := s.refreshTokens.FindByTokenHash(ctx, hash)
	if err != nil {
		return "", fmt.Errorf("AuthService.Refresh: %w", err)
	}
	if t == nil {
		return "", ErrTokenNotFound
	}
	if time.Now().After(t.ExpiresAt) {
		_ = s.refreshTokens.DeleteByTokenHash(ctx, hash)
		return "", ErrTokenExpired
	}

	if err := s.refreshTokens.DeleteByTokenHash(ctx, hash); err != nil {
		return "", fmt.Errorf("AuthService.Refresh: delete old token: %w", err)
	}

	accessToken, err := s.issueAccessToken(t.UserID, "free")
	if err != nil {
		return "", fmt.Errorf("AuthService.Refresh: %w", err)
	}
	return accessToken, nil
}

func (s *AuthService) issueAccessToken(userID, plan string) (string, error) {
	now := time.Now().UTC()
	cl := jwtClaims{
		UserID: userID,
		Plan:   plan,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(accessTokenTTL)),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, cl)
	signed, err := token.SignedString([]byte(s.jwtSecret))
	if err != nil {
		return "", fmt.Errorf("issueAccessToken: %w", err)
	}
	return signed, nil
}

func (s *AuthService) issueRefreshToken(ctx context.Context, userID string) (string, error) {
	raw := uuid.NewString()
	now := time.Now().UTC()
	t := &model.RefreshToken{
		ID:        uuid.NewString(),
		UserID:    userID,
		TokenHash: hashToken(raw),
		ExpiresAt: now.Add(refreshTokenTTL),
		CreatedAt: now,
	}
	if err := s.refreshTokens.Create(ctx, t); err != nil {
		return "", fmt.Errorf("issueRefreshToken: %w", err)
	}
	return raw, nil
}

func hashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}
