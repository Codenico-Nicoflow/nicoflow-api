package service

import (
	"context"
	"fmt"

	"nicoflow-api/internal/repository"
)

type UserService struct {
	users         repository.UserRepo
	refreshTokens repository.RefreshTokenRepo
}

func NewUserService(users repository.UserRepo, refreshTokens repository.RefreshTokenRepo) *UserService {
	return &UserService{users: users, refreshTokens: refreshTokens}
}

// Delete soft-deletes the user and invalidates all their refresh tokens.
func (s *UserService) Delete(ctx context.Context, userID string) error {
	if err := s.refreshTokens.DeleteAllByUserID(ctx, userID); err != nil {
		return fmt.Errorf("UserService.Delete: revoke tokens: %w", err)
	}
	if err := s.users.SoftDelete(ctx, userID); err != nil {
		return fmt.Errorf("UserService.Delete: %w", err)
	}
	return nil
}
