package search

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
)

// Repository defines the data-access contract for full-text search. Every query
// is row-level isolated by user_id and ranked by ts_rank against the STORED
// search_vector GIN columns (migrations 029 + 030; the 'simple' config keeps
// whole words so prefix matching works).
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

// toPrefixTSQuery turns a raw user term into a prefix-matching to_tsquery string
// so "testin" matches "testing" (type-ahead feel). Each whitespace-delimited word
// is stripped to alphanumerics, lowercased, suffixed with :* and AND-joined —
// e.g. "testin proj!" -> "testin:* & proj:*". Because we only ever emit
// sanitized `word:*` lexemes, the result is safe to hand to to_tsquery (which,
// unlike plainto_tsquery, would otherwise throw on raw punctuation / injection).
// Returns "" when the term has no alphanumeric content; callers treat that as a
// guaranteed-empty match rather than passing an invalid query to Postgres.
func toPrefixTSQuery(term string) string {
	fields := strings.FieldsFunc(strings.ToLower(term), func(r rune) bool {
		return !isAlnum(r)
	})
	lexemes := make([]string, 0, len(fields))
	for _, f := range fields {
		lexemes = append(lexemes, f+":*")
	}
	return strings.Join(lexemes, " & ")
}

func isAlnum(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') ||
		// keep non-ASCII letters/digits (e.g. Hebrew, Cyrillic) so multi-language search works.
		r > 127
}

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
		  AND t.search_vector @@ to_tsquery('simple', $2)
		  -- Terminal recurrence occurrences stay out of search (E-050): years of
		  -- history would drown the live results. Reachable from the rule detail
		  -- view instead. One-off done tasks are unaffected.
		  AND (t.recurrence_rule_id IS NULL OR t.status NOT IN ('done', 'missed'))
		ORDER BY ts_rank(t.search_vector, to_tsquery('simple', $2)) DESC, t.created_at DESC
		LIMIT $3`

	tsq := toPrefixTSQuery(term)
	if tsq == "" {
		return []TaskResult{}, nil
	}

	rows, err := r.db.Query(ctx, q, userID, tsq, limit, excerptLen)
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
		  AND p.search_vector @@ to_tsquery('simple', $2)
		ORDER BY ts_rank(p.search_vector, to_tsquery('simple', $2)) DESC, p.created_at DESC
		LIMIT $3`

	tsq := toPrefixTSQuery(term)
	if tsq == "" {
		return []ProjectResult{}, nil
	}

	rows, err := r.db.Query(ctx, q, userID, tsq, limit)
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
		  AND a.search_vector @@ to_tsquery('simple', $2)
		ORDER BY ts_rank(a.search_vector, to_tsquery('simple', $2)) DESC, a.created_at DESC
		LIMIT $3`

	tsq := toPrefixTSQuery(term)
	if tsq == "" {
		return []AreaResult{}, nil
	}

	rows, err := r.db.Query(ctx, q, userID, tsq, limit)
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
