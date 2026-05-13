package billing

import "github.com/jackc/pgx/v5/pgxpool"

// Repository defines the billing data access interface.
// Methods are added per story (E-029).
type Repository interface{}

type pgRepo struct{ db *pgxpool.Pool }

// NewRepository creates a new postgres-backed billing repository.
func NewRepository(db *pgxpool.Pool) Repository { return &pgRepo{db: db} }
