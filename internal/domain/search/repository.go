package search

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
)

// Repository defines the data-access contract for full-text search. Every query
// is row-level isolated by user_id and ranked by ts_rank against the STORED
// search_vector GIN columns (migration 029_search_vectors).
type Repository interface {
	SearchTasks(ctx context.Context, userID, term string, limit int) ([]TaskResult, error)
	SearchProjects(ctx context.Context, userID, term string, limit int) ([]ProjectResult, error)
	SearchAreas(ctx context.Context, userID, term string, limit int) ([]AreaResult, error)
}

type pgRepository struct {
	db *pgxpool.Pool
}

// NewRepository returns a PostgreSQL-backed Repository.
func NewRepository(db *pgxpool.Pool) Repository {
	return &pgRepository{db: db}
}

// excerptLen bounds the notes snippet returned per task hit (no highlighting — deferred).
const excerptLen = 100

func dbError(op string, err error) error {
	return apperror.New(500, apperror.ErrDatabaseError, fmt.Sprintf("search %s failed: %v", op, err))
}

func (r *pgRepository) SearchTasks(ctx context.Context, userID, term string, limit int) ([]TaskResult, error) {
	const q = `
		SELECT t.id,
		       t.title,
		       left(coalesce(t.notes, ''), $4) AS excerpt,
		       coalesce(p.name, '')            AS project_name,
		       coalesce(t.project_id, '')      AS project_id
		FROM tasks t
		LEFT JOIN projects p ON p.id = t.project_id
		WHERE t.user_id = $1
		  AND t.search_vector @@ plainto_tsquery('english', $2)
		ORDER BY ts_rank(t.search_vector, plainto_tsquery('english', $2)) DESC, t.created_at DESC
		LIMIT $3`

	rows, err := r.db.Query(ctx, q, userID, term, limit, excerptLen)
	if err != nil {
		return nil, dbError("tasks query", err)
	}
	defer rows.Close()

	results := make([]TaskResult, 0, limit)
	for rows.Next() {
		var t TaskResult
		if err := rows.Scan(&t.ID, &t.Title, &t.Excerpt, &t.ProjectName, &t.ProjectID); err != nil {
			return nil, dbError("tasks scan", err)
		}
		results = append(results, t)
	}
	if err := rows.Err(); err != nil {
		return nil, dbError("tasks rows", err)
	}
	return results, nil
}

func (r *pgRepository) SearchProjects(ctx context.Context, userID, term string, limit int) ([]ProjectResult, error) {
	const q = `
		SELECT p.id,
		       p.name,
		       coalesce(a.name, '') AS area_name
		FROM projects p
		LEFT JOIN areas a ON a.id = p.area_id
		WHERE p.user_id = $1
		  AND p.search_vector @@ plainto_tsquery('english', $2)
		ORDER BY ts_rank(p.search_vector, plainto_tsquery('english', $2)) DESC, p.created_at DESC
		LIMIT $3`

	rows, err := r.db.Query(ctx, q, userID, term, limit)
	if err != nil {
		return nil, dbError("projects query", err)
	}
	defer rows.Close()

	results := make([]ProjectResult, 0, limit)
	for rows.Next() {
		var p ProjectResult
		if err := rows.Scan(&p.ID, &p.Name, &p.AreaName); err != nil {
			return nil, dbError("projects scan", err)
		}
		results = append(results, p)
	}
	if err := rows.Err(); err != nil {
		return nil, dbError("projects rows", err)
	}
	return results, nil
}

func (r *pgRepository) SearchAreas(ctx context.Context, userID, term string, limit int) ([]AreaResult, error) {
	const q = `
		SELECT a.id, a.name
		FROM areas a
		WHERE a.user_id = $1
		  AND a.search_vector @@ plainto_tsquery('english', $2)
		ORDER BY ts_rank(a.search_vector, plainto_tsquery('english', $2)) DESC, a.created_at DESC
		LIMIT $3`

	rows, err := r.db.Query(ctx, q, userID, term, limit)
	if err != nil {
		return nil, dbError("areas query", err)
	}
	defer rows.Close()

	results := make([]AreaResult, 0, limit)
	for rows.Next() {
		var a AreaResult
		if err := rows.Scan(&a.ID, &a.Name); err != nil {
			return nil, dbError("areas scan", err)
		}
		results = append(results, a)
	}
	if err := rows.Err(); err != nil {
		return nil, dbError("areas rows", err)
	}
	return results, nil
}
