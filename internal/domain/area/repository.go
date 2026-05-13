package area

import "github.com/jackc/pgx/v5/pgxpool"

// Repository defines the area data access interface.
// Methods are added per story (E-011).
type Repository interface{}

type pgRepo struct{ db *pgxpool.Pool }

// NewRepository creates a new postgres-backed area repository.
func NewRepository(db *pgxpool.Pool) Repository { return &pgRepo{db: db} }
