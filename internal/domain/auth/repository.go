package auth

import "github.com/jackc/pgx/v5/pgxpool"

// Repository defines the auth data access interface.
// Methods are added per story (E-009).
type Repository interface{}

type pgRepo struct{ db *pgxpool.Pool }

// NewRepository creates a new postgres-backed auth repository.
func NewRepository(db *pgxpool.Pool) Repository { return &pgRepo{db: db} }
