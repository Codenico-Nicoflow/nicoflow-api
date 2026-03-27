package service

import (
	"context"

	"nicoflow-api/internal/model"
	"nicoflow-api/internal/repository"
)

type AuthService struct {
	users         repository.UserRepo
	refreshTokens repository.RefreshTokenRepo
	jwtSecret     string
}

func NewAuthService(users repository.UserRepo, refreshTokens repository.RefreshTokenRepo, jwtSecret string) *AuthService {
	return &AuthService{users: users, refreshTokens: refreshTokens, jwtSecret: jwtSecret}
}

func (s *AuthService) Register(ctx context.Context, email, password string) (*model.User, error) {
	panic("not implemented")
}

func (s *AuthService) Login(ctx context.Context, email, password string) (accessToken, refreshToken string, err error) {
	panic("not implemented")
}

func (s *AuthService) Refresh(ctx context.Context, refreshToken string) (accessToken string, err error) {
	panic("not implemented")
}
