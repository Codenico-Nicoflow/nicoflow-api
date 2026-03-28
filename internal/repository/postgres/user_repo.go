package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"nicoflow-api/internal/model"
)

type UserRepo struct {
	db *pgxpool.Pool
}

func NewUserRepo(db *pgxpool.Pool) *UserRepo {
	return &UserRepo{db: db}
}

func (r *UserRepo) Create(ctx context.Context, u *model.User) error {
	_, err := r.db.Exec(ctx,
		`INSERT INTO users (id, email, password_hash, name, timezone, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		u.ID, u.Email, u.PasswordHash, u.Name, u.Timezone, u.CreatedAt, u.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("UserRepo.Create: %w", err)
	}
	return nil
}

func (r *UserRepo) FindByEmail(ctx context.Context, email string) (*model.User, error) {
	u := &model.User{}
	err := r.db.QueryRow(ctx,
		`SELECT id, email, password_hash, name, timezone, created_at, updated_at
		 FROM users WHERE email = $1`,
		email,
	).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.Name, &u.Timezone, &u.CreatedAt, &u.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("UserRepo.FindByEmail: %w", err)
	}
	return u, nil
}

func (r *UserRepo) FindByID(ctx context.Context, id string) (*model.User, error) {
	u := &model.User{}
	err := r.db.QueryRow(ctx,
		`SELECT id, email, password_hash, name, timezone, created_at, updated_at
		 FROM users WHERE id = $1`,
		id,
	).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.Name, &u.Timezone, &u.CreatedAt, &u.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("UserRepo.FindByID: %w", err)
	}
	return u, nil
}

// TODO: implement Delete user
