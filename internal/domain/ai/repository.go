package ai

import "github.com/jackc/pgx/v5/pgxpool"

// Repository defines the AI assistant data access interface.
// Methods are added per story (E-026).
type Repository interface{}

type pgRepo struct{ db *pgxpool.Pool }

// NewRepository creates a new postgres-backed AI repository.
func NewRepository(db *pgxpool.Pool) Repository { return &pgRepo{db: db} }
