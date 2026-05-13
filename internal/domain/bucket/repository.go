package bucket

import "github.com/jackc/pgx/v5/pgxpool"

// Repository defines the bucket (inbox) data access interface.
// Methods are added per story (E-015).
type Repository interface{}

type pgRepo struct{ db *pgxpool.Pool }

// NewRepository creates a new postgres-backed bucket repository.
func NewRepository(db *pgxpool.Pool) Repository { return &pgRepo{db: db} }
