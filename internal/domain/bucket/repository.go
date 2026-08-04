package bucket

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
)

// Repository defines the data-access contract for the bucket (inbox) domain.
type Repository interface {
	Create(ctx context.Context, b Bucket) (Bucket, error)
	ListByUser(ctx context.Context, userID string) ([]Bucket, error)
	GetByID(ctx context.Context, userID, id string) (Bucket, error)
	// UpdateContent edits the content of an UNPROCESSED item only.
	// A processed item returns 409 CONFLICT; a missing/foreign item returns 404.
	UpdateContent(ctx context.Context, userID, id, content string) (Bucket, error)
	Delete(ctx context.Context, userID, id string) error
	// CountUnprocessed returns how many of the user's inbox items are still
	// unprocessed (processed_at IS NULL) — zero is the inbox_zero signal.
	CountUnprocessed(ctx context.Context, userID string) (int, error)
	// MarkProcessed stamps result and the produced-entity ids on an UNPROCESSED
	// item only. Already-processed returns 409 CONFLICT (the concurrency
	// backstop).
	MarkProcessed(ctx context.Context, userID, id, result string, refs ProcessedRefs) (Bucket, error)
}

type pgRepo struct{ db *pgxpool.Pool }

// NewRepository creates a new postgres-backed bucket repository.
func NewRepository(db *pgxpool.Pool) Repository { return &pgRepo{db: db} }

const bucketSelectCols = ` id, user_id, content, processing_result, project_id,
	created_task_id, created_note_id, processed_at, created_at, updated_at `

func scanBucket(row pgx.Row, b *Bucket) error {
	return row.Scan(
		&b.ID, &b.UserID, &b.Content, &b.ProcessingResult, &b.ProjectID,
		&b.CreatedTaskID, &b.CreatedNoteID, &b.ProcessedAt, &b.CreatedAt, &b.UpdatedAt,
	)
}

func (r *pgRepo) Create(ctx context.Context, b Bucket) (Bucket, error) {
	err := scanBucket(
		r.db.QueryRow(ctx, `
			INSERT INTO bucket (id, user_id, content, created_at, updated_at)
			VALUES (@id, @userID, @content, NOW(), NOW())
			RETURNING`+bucketSelectCols,
			pgx.NamedArgs{"id": b.ID, "userID": b.UserID, "content": b.Content},
		),
		&b,
	)
	if err != nil {
		return Bucket{}, fmt.Errorf("bucket.Create: %w", err)
	}
	return b, nil
}

func (r *pgRepo) ListByUser(ctx context.Context, userID string) ([]Bucket, error) {
	rows, err := r.db.Query(ctx,
		`SELECT`+bucketSelectCols+`FROM bucket WHERE user_id = @userID
		 ORDER BY created_at DESC, id DESC`,
		pgx.NamedArgs{"userID": userID},
	)
	if err != nil {
		return nil, fmt.Errorf("bucket.ListByUser: %w", err)
	}
	defer rows.Close()

	var items []Bucket
	for rows.Next() {
		var b Bucket
		if err := scanBucket(rows, &b); err != nil {
			return nil, fmt.Errorf("bucket.ListByUser scan: %w", err)
		}
		items = append(items, b)
	}
	return items, rows.Err()
}

func (r *pgRepo) CountUnprocessed(ctx context.Context, userID string) (int, error) {
	var count int
	err := r.db.QueryRow(ctx,
		`SELECT COUNT(*) FROM bucket WHERE user_id = @userID AND processed_at IS NULL`,
		pgx.NamedArgs{"userID": userID},
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("bucket.CountUnprocessed: %w", err)
	}
	return count, nil
}

func (r *pgRepo) GetByID(ctx context.Context, userID, id string) (Bucket, error) {
	var b Bucket
	err := scanBucket(
		r.db.QueryRow(ctx,
			`SELECT`+bucketSelectCols+`FROM bucket WHERE id = @id AND user_id = @userID`,
			pgx.NamedArgs{"id": id, "userID": userID},
		),
		&b,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Bucket{}, errNotFound()
		}
		return Bucket{}, fmt.Errorf("bucket.GetByID: %w", err)
	}
	return b, nil
}

func (r *pgRepo) UpdateContent(ctx context.Context, userID, id, content string) (Bucket, error) {
	var b Bucket
	err := scanBucket(
		r.db.QueryRow(ctx, `
			UPDATE bucket SET content = @content, updated_at = NOW()
			WHERE id = @id AND user_id = @userID AND processed_at IS NULL
			RETURNING`+bucketSelectCols,
			pgx.NamedArgs{"content": content, "id": id, "userID": userID},
		),
		&b,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Bucket{}, r.disambiguateGuardMiss(ctx, userID, id)
		}
		return Bucket{}, fmt.Errorf("bucket.UpdateContent: %w", err)
	}
	return b, nil
}

func (r *pgRepo) MarkProcessed(ctx context.Context, userID, id, result string, refs ProcessedRefs) (Bucket, error) {
	var b Bucket
	err := scanBucket(
		r.db.QueryRow(ctx, `
			UPDATE bucket SET
				processing_result = @result,
				created_task_id   = @taskID,
				created_note_id   = @noteID,
				project_id        = @projectID,
				processed_at      = NOW(),
				updated_at        = NOW()
			WHERE id = @id AND user_id = @userID AND processed_at IS NULL
			RETURNING`+bucketSelectCols,
			pgx.NamedArgs{
				"result": result, "taskID": refs.TaskID, "noteID": refs.NoteID,
				"projectID": refs.ProjectID, "id": id, "userID": userID,
			},
		),
		&b,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Bucket{}, r.disambiguateGuardMiss(ctx, userID, id)
		}
		return Bucket{}, fmt.Errorf("bucket.MarkProcessed: %w", err)
	}
	return b, nil
}

func (r *pgRepo) Delete(ctx context.Context, userID, id string) error {
	tag, err := r.db.Exec(ctx,
		`DELETE FROM bucket WHERE id = @id AND user_id = @userID`,
		pgx.NamedArgs{"id": id, "userID": userID},
	)
	if err != nil {
		return fmt.Errorf("bucket.Delete: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return errNotFound()
	}
	return nil
}

// disambiguateGuardMiss decides why a `processed_at IS NULL`-guarded write hit
// zero rows: the item is missing/not-owned (404) or already processed (409).
func (r *pgRepo) disambiguateGuardMiss(ctx context.Context, userID, id string) error {
	var processed bool
	err := r.db.QueryRow(ctx,
		`SELECT processed_at IS NOT NULL FROM bucket WHERE id = @id AND user_id = @userID`,
		pgx.NamedArgs{"id": id, "userID": userID},
	).Scan(&processed)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return errNotFound()
		}
		return fmt.Errorf("bucket.disambiguateGuardMiss: %w", err)
	}
	if processed {
		return apperror.New(http.StatusConflict, apperror.ErrConflict, "bucket item is already processed")
	}
	// Row exists and is unprocessed but the guarded write still missed — treat as
	// a lost race (another request processed and rolled back, or concurrent edit).
	return apperror.New(http.StatusConflict, apperror.ErrConflict, "bucket item could not be updated")
}

func errNotFound() error {
	return apperror.New(http.StatusNotFound, apperror.ErrResourceNotFound, "bucket item not found")
}
