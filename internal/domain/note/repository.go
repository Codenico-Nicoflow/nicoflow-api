package note

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
	"github.com/nicoflow/nicoflow-api/pkg/cursorutil"
)

type pgRepo struct{ db *pgxpool.Pool }

// NewRepository creates a postgres-backed note repository.
func NewRepository(db *pgxpool.Pool) Repository { return &pgRepo{db: db} }

// fullCols is the scalar read: the whole document. listCols swaps content for
// content_text — a list must never pull large JSONB bodies it won't render.
const (
	fullCols = ` id, user_id, project_id, title, content, content_text, version, created_at, updated_at `
	listCols = ` id, user_id, project_id, title, content_text, version, created_at, updated_at `
)

func scanFull(row pgx.Row, n *Note) error {
	return row.Scan(&n.ID, &n.UserID, &n.ProjectID, &n.Title, &n.Content, &n.ContentText,
		&n.Version, &n.CreatedAt, &n.UpdatedAt)
}

func scanList(row pgx.Row, n *Note) error {
	return row.Scan(&n.ID, &n.UserID, &n.ProjectID, &n.Title, &n.ContentText,
		&n.Version, &n.CreatedAt, &n.UpdatedAt)
}

func notFound() error {
	return apperror.New(http.StatusNotFound, apperror.ErrResourceNotFound, "note not found")
}

// ListByProject is scoped by user_id as well as project_id: matching only the
// project would let a guessed project id list someone else's notes.
// Keyset pagination on (updated_at DESC, id DESC): an edit re-tops the list,
// so a note being edited mid-scroll can appear on two consecutive pages — this
// is documented and acceptable, not a bug.
func (r *pgRepo) ListByProject(ctx context.Context, userID, projectID string, f ListNotesFilter) ([]Note, string, error) {
	limit := f.Limit
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	cursorUpdated, cursorID, err := cursorutil.DecodeTime(f.Cursor)
	if err != nil {
		return nil, "", apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidInput, "invalid cursor")
	}

	rows, err := r.db.Query(ctx,
		`SELECT`+listCols+`FROM notes
		 WHERE user_id = $1 AND project_id = $2
		   AND (NOT $3 OR (updated_at, id) < ($4, $5))
		 ORDER BY updated_at DESC, id DESC
		 LIMIT $6`,
		userID, projectID, f.Cursor != "", cursorUpdated, cursorID, limit+1,
	)
	if err != nil {
		return nil, "", fmt.Errorf("note.ListByProject: %w", err)
	}
	defer rows.Close()

	var out []Note
	for rows.Next() {
		var n Note
		if err := scanList(rows, &n); err != nil {
			return nil, "", fmt.Errorf("note.ListByProject scan: %w", err)
		}
		out = append(out, n)
	}
	if err := rows.Err(); err != nil {
		return nil, "", fmt.Errorf("note.ListByProject rows: %w", err)
	}

	var next string
	if len(out) > limit {
		last := out[limit-1]
		next = cursorutil.EncodeTime(last.UpdatedAt, last.ID)
		out = out[:limit]
	}
	return out, next, nil
}

func (r *pgRepo) GetByID(ctx context.Context, userID, id string) (Note, error) {
	var n Note
	err := scanFull(
		r.db.QueryRow(ctx,
			`SELECT`+fullCols+`FROM notes WHERE id = $1 AND user_id = $2`,
			id, userID,
		),
		&n,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		// Not-owned is reported exactly like missing — never an existence leak.
		return Note{}, notFound()
	}
	if err != nil {
		return Note{}, fmt.Errorf("note.GetByID: %w", err)
	}
	return n, nil
}

// Create stamps version/created_at/updated_at from the column defaults so the
// database stays the single source of truth for them.
func (r *pgRepo) Create(ctx context.Context, n Note) (Note, error) {
	var out Note
	err := scanFull(
		r.db.QueryRow(ctx,
			`INSERT INTO notes (id, user_id, project_id, title, content, content_text)
			 VALUES ($1, $2, $3, $4, $5, $6)
			 RETURNING`+fullCols,
			n.ID, n.UserID, n.ProjectID, n.Title, n.Content, n.ContentText,
		),
		&out,
	)
	if err != nil {
		return Note{}, fmt.Errorf("note.Create: %w", err)
	}
	return out, nil
}

// Update is the optimistic-concurrency guard. Matching id + user_id + version in
// one statement means a stale save loses the race in the database rather than in
// application code: 0 rows affected is the signal, the same idiom refresh-token
// reuse detection uses. version is incremented in the same statement so two
// concurrent saves at the same base version cannot both win.
func (r *pgRepo) Update(ctx context.Context, p UpdateParams) (Note, bool, error) {
	var out Note
	err := scanFull(
		r.db.QueryRow(ctx,
			`UPDATE notes
			    SET title = $4, content = $5, content_text = $6,
			        version = version + 1, updated_at = NOW()
			  WHERE id = $1 AND user_id = $2 AND version = $3
			  RETURNING`+fullCols,
			p.ID, p.UserID, p.Version, p.Title, p.Content, p.ContentText,
		),
		&out,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Note{}, false, nil
	}
	if err != nil {
		return Note{}, false, fmt.Errorf("note.Update: %w", err)
	}
	return out, true, nil
}

func (r *pgRepo) Delete(ctx context.Context, userID, id string) (bool, error) {
	tag, err := r.db.Exec(ctx,
		`DELETE FROM notes WHERE id = $1 AND user_id = $2`,
		id, userID,
	)
	if err != nil {
		return false, fmt.Errorf("note.Delete: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// ExistsByID is deliberately not user-scoped — the GC sweep asks "does this note
// still exist at all?" across every user. Sweep-only; never reachable from a
// request path.
func (r *pgRepo) ExistsByID(ctx context.Context, id string) (bool, error) {
	var exists bool
	err := r.db.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM notes WHERE id = $1)`,
		id,
	).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("note.ExistsByID: %w", err)
	}
	return exists, nil
}

func (r *pgRepo) ExistsForUser(ctx context.Context, userID, noteID string) (bool, error) {
	var exists bool
	err := r.db.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM notes WHERE id = $1 AND user_id = $2)`,
		noteID, userID,
	).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("note.ExistsForUser: %w", err)
	}
	return exists, nil
}
