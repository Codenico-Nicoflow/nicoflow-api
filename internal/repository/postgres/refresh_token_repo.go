package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"nicoflow-api/internal/model"
)

type RefreshTokenRepo struct {
	db *pgxpool.Pool
}

func NewRefreshTokenRepo(db *pgxpool.Pool) *RefreshTokenRepo {
	return &RefreshTokenRepo{db: db}
}

func (r *RefreshTokenRepo) Create(ctx context.Context, t *model.RefreshToken) error {
	_, err := r.db.Exec(ctx,
		`INSERT INTO refresh_tokens (id, user_id, token_hash, expires_at, created_at)
		 VALUES ($1, $2, $3, $4, $5)`,
		t.ID, t.UserID, t.TokenHash, t.ExpiresAt, t.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("RefreshTokenRepo.Create: %w", err)
	}
	return nil
}

func (r *RefreshTokenRepo) FindByTokenHash(ctx context.Context, hash string) (*model.RefreshToken, error) {
	t := &model.RefreshToken{}
	err := r.db.QueryRow(ctx,
		`SELECT id, user_id, token_hash, expires_at, created_at
		 FROM refresh_tokens WHERE token_hash = $1`,
		hash,
	).Scan(&t.ID, &t.UserID, &t.TokenHash, &t.ExpiresAt, &t.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("RefreshTokenRepo.FindByTokenHash: %w", err)
	}
	return t, nil
}

func (r *RefreshTokenRepo) DeleteByTokenHash(ctx context.Context, hash string) error {
	_, err := r.db.Exec(ctx, `DELETE FROM refresh_tokens WHERE token_hash = $1`, hash)
	if err != nil {
		return fmt.Errorf("RefreshTokenRepo.DeleteByTokenHash: %w", err)
	}
	return nil
}

func (r *RefreshTokenRepo) DeleteAllByUserID(ctx context.Context, userID string) error {
	_, err := r.db.Exec(ctx, `DELETE FROM refresh_tokens WHERE user_id = $1`, userID)
	if err != nil {
		return fmt.Errorf("RefreshTokenRepo.DeleteAllByUserID: %w", err)
	}
	return nil
}
