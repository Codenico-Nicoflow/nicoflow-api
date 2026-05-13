package task

import "github.com/jackc/pgx/v5/pgxpool"

// Repository defines the task data access interface.
// Methods are added per story (E-013).
type Repository interface{}

type pgRepo struct{ db *pgxpool.Pool }

// NewRepository creates a new postgres-backed task repository.
func NewRepository(db *pgxpool.Pool) Repository { return &pgRepo{db: db} }
